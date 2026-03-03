package wrike_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	wrikeext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/wrike"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrikeListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "acc_1", "name": "My Wrike Account"},
			},
		})
	}))
	defer srv.Close()

	e := wrikeext.New("fake-token", wrikeext.WithBaseURL(srv.URL))
	ws, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, ws, 1)
	assert.Equal(t, "acc_1", ws[0].ID)
	assert.Equal(t, "My Wrike Account", ws[0].Name)
}

func TestWrikeExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id": "IEABCDE", "title": "First Task",
					"description": "<p>Task description</p>",
					"status":      "Active",
					"dates":       map[string]interface{}{"due": "2026-04-01"},
				},
			},
		})
	}))
	defer srv.Close()

	e := wrikeext.New("fake-token", wrikeext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "folder_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "folder_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Task", proj.Rows[0].Cells[0].Value)
	assert.Equal(t, "Task description", proj.Rows[0].Cells[1].Value)
}
