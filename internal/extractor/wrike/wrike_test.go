package wrike_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	wrikeext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/wrike"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrikeListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contacts", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("me"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{
					"accountId": "acc_1",
					"profiles": []map[string]interface{}{
						{"accountId": "acc_1", "name": "My Wrike Account"},
					},
				},
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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

func TestWrikeListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{"id": "FOLDER_1", "title": "My Project", "project": map[string]interface{}{}},
				{"id": "FOLDER_2", "title": "Just a Folder"},
			},
		})
	}))
	defer srv.Close()

	e := wrikeext.New("fake-token", wrikeext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "acc_1")
	require.NoError(t, err)
	assert.Len(t, projects, 1, "only folders with 'project' field should be returned")
	assert.Equal(t, "FOLDER_1", projects[0].ID)
	assert.Equal(t, "My Project", projects[0].Name)
}

func TestWrikeExtractProjectFolderName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// First call is the folder name fetch, second is the tasks fetch
		if !strings.Contains(r.URL.RawQuery, "fields") && !strings.Contains(r.URL.Path, "/tasks") {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": []map[string]interface{}{
					{"id": "folder_1", "title": "My Real Folder Name"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{
					"id": "task_1", "title": "Task One",
					"description": "", "status": "Active",
					"dates": map[string]interface{}{"due": "2026-04-01"},
					"parentIds": []interface{}{},
					"responsibles": []interface{}{"user_1"},
				},
			},
		})
	}))
	defer srv.Close()

	e := wrikeext.New("fake-token", wrikeext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "folder_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "My Real Folder Name", proj.Name)
}
