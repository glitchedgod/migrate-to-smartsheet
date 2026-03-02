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

func TestNotionExtractProjectPropertyTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"id": "page_1",
					"properties": map[string]interface{}{
						"Name": map[string]interface{}{
							"title": []map[string]interface{}{{"plain_text": "My Page"}},
						},
						"Status": map[string]interface{}{
							"select": map[string]interface{}{"name": "In Progress"},
						},
						"Due": map[string]interface{}{
							"date": map[string]interface{}{"start": "2026-04-01"},
						},
						"Done": map[string]interface{}{
							"checkbox": true,
						},
						"Score": map[string]interface{}{
							"number": float64(42),
						},
						"Notes": map[string]interface{}{
							"rich_text": []map[string]interface{}{{"plain_text": "Some notes"}},
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
	assert.Len(t, proj.Rows, 1)

	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, "My Page", cellMap["Name"])
	assert.Equal(t, "In Progress", cellMap["Status"])
	assert.Equal(t, "2026-04-01", cellMap["Due"])
	assert.Equal(t, "true", cellMap["Done"])
	assert.Equal(t, "42", cellMap["Score"])
	assert.Equal(t, "Some notes", cellMap["Notes"])
}

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
