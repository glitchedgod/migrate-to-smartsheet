package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	jiraext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraListWorkspaces(t *testing.T) {
	e := jiraext.New("user@example.com", "api-token", "https://myorg.atlassian.net")
	ws, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, ws, 1)
	assert.Equal(t, "https://myorg.atlassian.net", ws[0].ID)
}

func TestJiraExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/3/field" {
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "summary", "name": "Summary"},
				{"id": "status", "name": "Status"},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"id": "10001", "key": "PROJ-1",
					"fields": map[string]interface{}{
						"summary": "First Issue",
						"status":  map[string]interface{}{"name": "To Do"},
					},
				},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	e := jiraext.New("user@example.com", "api-token", srv.URL, jiraext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "PROJ", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "PROJ", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Issue", proj.Rows[0].Cells[0].Value)
}
