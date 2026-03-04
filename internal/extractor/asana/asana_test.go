package asana_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	asanaext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/asana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsanaExtractProjectExcludesCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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

func TestAsanaListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/workspaces/ws_1/projects")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{"gid": "proj_1", "name": "Alpha Project"},
				{"gid": "proj_2", "name": "Beta Project"},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "ws_1")
	require.NoError(t, err)
	assert.Len(t, projects, 2)
	assert.Equal(t, "proj_1", projects[0].ID)
	assert.Equal(t, "Alpha Project", projects[0].Name)
}

func TestAsanaExtractProjectHasCorrectName(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		// First call is for project name, second is for tasks
		if strings.Contains(r.URL.Path, "/projects/proj_1") && !strings.Contains(r.URL.Path, "/tasks") {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": map[string]interface{}{"gid": "proj_1", "name": "My Real Project Name"},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}}) //nolint:errcheck
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "ws_1", "proj_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "My Real Project Name", proj.Name)
}
