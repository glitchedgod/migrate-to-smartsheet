package monday_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	mondayext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/monday"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMondayListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"workspaces": []map[string]interface{}{
					{"id": "ws_1", "name": "Main Workspace"},
				},
			},
		})
	}))
	defer srv.Close()

	e := mondayext.New("fake-token", mondayext.WithBaseURL(srv.URL))
	ws, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, ws, 1)
	assert.Equal(t, "ws_1", ws[0].ID)
	assert.Equal(t, "Main Workspace", ws[0].Name)
}

func TestMondayExtractProjectPagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if callCount == 1 {
			// First call: return cursor
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": map[string]interface{}{
					"boards": []map[string]interface{}{
						{
							"id": "board_1", "name": "Big Board",
							"columns": []map[string]interface{}{
								{"id": "name", "title": "Name", "type": "name"},
							},
							"items_page": map[string]interface{}{
								"cursor": "cursor_abc",
								"items": []map[string]interface{}{
									{"id": "item_1", "name": "Item 1", "column_values": []interface{}{}},
								},
							},
						},
					},
				},
			})
		} else {
			// Second call: next_items_page, no cursor
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": map[string]interface{}{
					"next_items_page": map[string]interface{}{
						"cursor": nil,
						"items": []map[string]interface{}{
							{"id": "item_2", "name": "Item 2", "column_values": []interface{}{}},
						},
					},
				},
			})
		}
	}))
	defer srv.Close()

	e := mondayext.New("fake-token", mondayext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "board_1", extractor.Options{})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 2, "should have items from both pages")
	assert.Equal(t, 2, callCount, "should have made 2 API calls")
}

func TestMondayExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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

func TestMondayListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{"id": "ws_1", "name": "Main Workspace"},
				},
			},
		})
	}))
	defer srv.Close()

	e := mondayext.New("fake-token", mondayext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "ws_1", projects[0].ID)
	assert.Equal(t, "Main Workspace", projects[0].Name)
}

func TestMondayColumnNamesInCells(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{
						"id": "b1", "name": "Board",
						"columns": []map[string]interface{}{
							{"id": "status", "title": "Status", "type": "color"},
						},
						"items_page": map[string]interface{}{
							"cursor": nil,
							"items": []map[string]interface{}{
								{
									"id": "i1", "name": "Item 1",
									"column_values": []map[string]interface{}{
										{"id": "status", "text": "Done"},
									},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	e := mondayext.New("fake-token", mondayext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "b1", extractor.Options{})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 1)

	// Find the Status cell — should use column TITLE not raw ID
	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, "Done", cellMap["Status"], "cell should use column title 'Status', not raw id 'status'")
}
