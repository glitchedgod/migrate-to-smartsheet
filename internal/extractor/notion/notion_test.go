package notion_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	notionext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/notion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotionExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"id": "page_1",
					"properties": map[string]interface{}{
						"Name": map[string]interface{}{
							"title": []map[string]interface{}{{"plain_text": "First Page"}},
						},
					},
				},
			},
			"has_more": false,
		})
	}))
	defer srv.Close()

	e := notionext.New("fake-token", notionext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "db_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "db_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
}
