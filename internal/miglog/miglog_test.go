package miglog_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/miglog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readEntries(t *testing.T, path string) []miglog.Entry {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var entries []miglog.Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e miglog.Entry
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e))
		entries = append(entries, e)
	}
	require.NoError(t, sc.Err())
	return entries
}

func TestLoggerWritesNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	lg, err := miglog.New(path, "asana")
	require.NoError(t, err)

	lg.Info("migration started", "source", "asana")
	lg.SheetStart("My Project", 5, 100)
	lg.SheetComplete("My Project", 100, 2, 3)
	lg.Warn("attachment skipped", "name", "big.mp4", "reason", "exceeds 25MB")
	lg.Summary(1, 100, 1, 0, "success")
	require.NoError(t, lg.Close())

	entries := readEntries(t, path)
	require.Len(t, entries, 5)

	assert.Equal(t, miglog.LevelInfo, entries[0].Level)
	assert.Equal(t, "asana", entries[0].Platform)
	assert.Equal(t, "migration started", entries[0].Message)
	assert.Equal(t, "asana", entries[0].Fields["source"])

	assert.Equal(t, miglog.LevelInfo, entries[1].Level)
	assert.Equal(t, "My Project", entries[1].Project)
	assert.Equal(t, float64(5), entries[1].Fields["columns"])
	assert.Equal(t, float64(100), entries[1].Fields["rows"])

	assert.Equal(t, miglog.LevelWarn, entries[3].Level)
	assert.Equal(t, "big.mp4", entries[3].Fields["name"])

	assert.Equal(t, miglog.LevelSummary, entries[4].Level)
	assert.Equal(t, "success", entries[4].Fields["status"])
	assert.Equal(t, float64(100), entries[4].Fields["rows"])
}

func TestLoggerSheetFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fail.log")
	lg, err := miglog.New(path, "jira")
	require.NoError(t, err)
	lg.SheetFailed("PROJ", fmt.Errorf("API error 500"))
	require.NoError(t, lg.Close())

	entries := readEntries(t, path)
	require.Len(t, entries, 1)
	assert.Equal(t, miglog.LevelError, entries[0].Level)
	assert.Equal(t, "PROJ", entries[0].Project)
}

func TestLoggerFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	lg, err := miglog.New(path, "notion")
	require.NoError(t, err)
	defer func() { _ = lg.Close() }()
	assert.Equal(t, path, lg.FilePath())
}

func TestListErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "errors.log")
	lg, err := miglog.New(path, "airtable")
	require.NoError(t, err)

	lg.Info("migration started")
	lg.SheetStart("ProjectA", 3, 50)
	lg.SheetFailed("ProjectA", fmt.Errorf("API rate limit exceeded"))
	lg.Warn("attachment skipped", "name", "file.pdf", "reason", "too large")
	lg.AttachmentFailed("ProjectB", "image.png", fmt.Errorf("upload timeout"))
	lg.CommentFailed("ProjectC", fmt.Errorf("403 Forbidden"))
	lg.SheetComplete("ProjectD", 10, 0, 0)
	lg.Summary(2, 60, 1, 2, "partial")

	require.NoError(t, lg.Close())

	errs := lg.ListErrors()
	require.Len(t, errs, 3)

	// SheetFailed entry
	assert.Equal(t, "ProjectA", errs[0].Sheet)
	assert.Equal(t, "sheet migration failed", errs[0].Message)
	assert.Equal(t, "API rate limit exceeded", errs[0].ErrText)

	// AttachmentFailed entry
	assert.Equal(t, "ProjectB", errs[1].Sheet)
	assert.Equal(t, "attachment upload failed", errs[1].Message)
	assert.Equal(t, "upload timeout", errs[1].ErrText)

	// CommentFailed entry
	assert.Equal(t, "ProjectC", errs[2].Sheet)
	assert.Equal(t, "comment post failed", errs[2].Message)
	assert.Equal(t, "403 Forbidden", errs[2].ErrText)
}

func TestListErrorsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noerrors.log")
	lg, err := miglog.New(path, "trello")
	require.NoError(t, err)

	lg.Info("all good")
	lg.SheetComplete("Clean", 5, 0, 0)
	lg.Summary(1, 5, 0, 0, "success")
	require.NoError(t, lg.Close())

	errs := lg.ListErrors()
	assert.Empty(t, errs)
}

func TestListErrorsMissingFile(t *testing.T) {
	// Logger pointing at a path that doesn't exist after Close — simulate by
	// using a path that was never created.
	path := filepath.Join(t.TempDir(), "ghost.log")
	lg, err := miglog.New(path, "wrike")
	require.NoError(t, err)
	require.NoError(t, lg.Close())
	// Remove the file to simulate a missing log
	require.NoError(t, os.Remove(path))

	errs := lg.ListErrors()
	assert.Nil(t, errs)
}
