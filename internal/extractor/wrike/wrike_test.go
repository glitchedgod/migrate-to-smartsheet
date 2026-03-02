package wrike_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	wrikeext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/wrike"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
