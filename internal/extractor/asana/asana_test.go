package asana_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	asanaext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/asana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsanaExtractProjectExcludesCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"gid": "t1", "name": "Active Task", "completed": false, "notes": "", "tags": []interface{}{}},
				{"gid": "t2", "name": "Done Task", "completed": true, "notes": "", "tags": []interface{}{}},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "ws", "proj", extractor.Options{IncludeArchived: false})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "Active Task", proj.Rows[0].Cells[0].Value)
}

func TestAsanaExtractProjectIncludesCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"gid": "t1", "name": "Active Task", "completed": false, "notes": "", "tags": []interface{}{}},
				{"gid": "t2", "name": "Done Task", "completed": true, "notes": "", "tags": []interface{}{}},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "ws", "proj", extractor.Options{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 2)
}

func TestAsanaListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"gid": "ws_1", "name": "My Workspace"},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	workspaces, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, workspaces, 1)
	assert.Equal(t, "ws_1", workspaces[0].ID)
}

func TestAsanaExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"gid": "task_1", "name": "First Task", "notes": "notes",
					"completed": false, "due_on": "2026-04-01",
					"assignee": map[string]interface{}{"gid": "u1", "email": "a@a.com"},
					"tags": []interface{}{},
				},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "ws_1", "proj_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "proj_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Task", proj.Rows[0].Cells[0].Value)
}
