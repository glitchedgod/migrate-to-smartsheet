package state_test

import (
	"os"
	"testing"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := &state.MigrationState{
		Source:          "asana",
		StartedAt:       time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC),
		CompletedSheets: []string{"sheet_1", "sheet_2"},
	}
	path := dir + "/.migrate-state-2026-03-01.json"
	require.NoError(t, state.Save(path, s))

	loaded, err := state.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "asana", loaded.Source)
	assert.Equal(t, []string{"sheet_1", "sheet_2"}, loaded.CompletedSheets)
}

func TestLoadMissing(t *testing.T) {
	_, err := state.Load("/nonexistent/path.json")
	assert.True(t, os.IsNotExist(err))
}

func TestIsCompleted(t *testing.T) {
	s := &state.MigrationState{CompletedSheets: []string{"a", "b"}}
	assert.True(t, s.IsCompleted("a"))
	assert.False(t, s.IsCompleted("c"))
}

func TestMarkCompleted(t *testing.T) {
	s := &state.MigrationState{}
	s.MarkCompleted("proj1")
	s.MarkCompleted("proj1")
	assert.Len(t, s.CompletedSheets, 1)
}

func TestSaveToInvalidPath(t *testing.T) {
	s := &state.MigrationState{Source: "asana"}
	err := state.Save("/nonexistent/dir/file.json", s)
	assert.Error(t, err)
}

func TestLoadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.json"
	require.NoError(t, os.WriteFile(path, []byte("{invalid json"), 0600))
	_, err := state.Load(path)
	assert.Error(t, err)
}

func TestUpdatePartialSheet(t *testing.T) {
	s := &state.MigrationState{Source: "asana"}
	s.UpdatePartialSheet("proj_1", 999, 250)
	assert.NotNil(t, s.PartialSheet)
	assert.Equal(t, "proj_1", s.PartialSheet.SourceID)
	assert.Equal(t, int64(999), s.PartialSheet.SmartsheetID)
	assert.Equal(t, 250, s.PartialSheet.LastCompletedRow)
}

func TestClearPartialSheet(t *testing.T) {
	s := &state.MigrationState{
		PartialSheet: &state.PartialSheet{SourceID: "p1"},
	}
	s.ClearPartialSheet()
	assert.Nil(t, s.PartialSheet)
}

func TestUpdatePartialSheetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := &state.MigrationState{Source: "jira"}
	s.UpdatePartialSheet("PROJ", 12345, 100)

	path := dir + "/state.json"
	require.NoError(t, state.Save(path, s))

	loaded, err := state.Load(path)
	require.NoError(t, err)
	require.NotNil(t, loaded.PartialSheet)
	assert.Equal(t, "PROJ", loaded.PartialSheet.SourceID)
	assert.Equal(t, int64(12345), loaded.PartialSheet.SmartsheetID)
	assert.Equal(t, 100, loaded.PartialSheet.LastCompletedRow)
}
