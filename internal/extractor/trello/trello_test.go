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
