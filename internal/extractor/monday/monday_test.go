package monday_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	mondayext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/monday"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMondayExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{
						"id": "board_1", "name": "My Board",
						"columns": []map[string]interface{}{
							{"id": "name", "title": "Name", "type": "name"},
						},
						"items_page": map[string]interface{}{
							"items": []map[string]interface{}{
								{"id": "item_1", "name": "First Item", "column_values": []interface{}{}},
							},
							"cursor": nil,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	e := mondayext.New("fake-token", mondayext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "board_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "board_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Item", proj.Rows[0].Cells[0].Value)
}
