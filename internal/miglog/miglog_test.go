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
