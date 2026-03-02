package trello_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	trelloext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/trello"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrelloExtractProjectExcludesClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
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
		json.NewEncoder(w).Encode([]map[string]interface{}{
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
		json.NewEncoder(w).Encode([]map[string]interface{}{
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
		json.NewEncoder(w).Encode([]map[string]interface{}{
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
