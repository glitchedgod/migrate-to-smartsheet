package trello_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	trelloext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/trello"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrelloExtractProjectExcludesClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "c1", "name": "Open Card", "desc": "", "closed": false, "idList": "l1"},
			{"id": "c2", "name": "Closed Card", "desc": "", "closed": true, "idList": "l1"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "board_1", extractor.Options{IncludeArchived: false})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "Open Card", proj.Rows[0].Cells[0].Value)
}

func TestTrelloExtractProjectDueDate(t *testing.T) {
	due := "2026-04-01T00:00:00.000Z"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "c1", "name": "Card With Due", "desc": "", "closed": false, "idList": "l1", "due": due},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "board_1", extractor.Options{})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 1)
	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, due, cellMap["Due Date"])
}

func TestTrelloListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "org_1", "displayName": "My Org"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	workspaces, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, workspaces, 1)
	assert.Equal(t, "org_1", workspaces[0].ID)
}

func TestTrelloExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "card_1", "name": "First Card", "desc": "desc", "closed": false, "idList": "list_1"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "org_1", "board_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "board_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
}

func TestTrelloListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "board_1", "name": "My Board"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "org_1")
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "board_1", projects[0].ID)
	assert.Equal(t, "My Board", projects[0].Name)
}

func TestTrelloExtractProjectListNamesAndLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/lists") {
			json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
				{"id": "list_1", "name": "To Do"},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/cards") {
			json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
				{
					"id": "c1", "name": "Card", "desc": "", "closed": false,
					"idList": "list_1",
					"labels": []map[string]interface{}{{"name": "urgent", "color": "red"}},
				},
			})
			return
		}
		// board name
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "board_1", "name": "My Board"}) //nolint:errcheck
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "org_1", "board_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "My Board", proj.Name)

	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, "To Do", cellMap["List"], "list ID should resolve to name")
	labels, ok := cellMap["Labels"].([]string)
	assert.True(t, ok)
	assert.Contains(t, labels, "urgent")
}
