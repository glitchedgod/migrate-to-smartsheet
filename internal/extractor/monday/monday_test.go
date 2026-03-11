package monday_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMondayStatusOptionsFromSettingsStr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{
						"id": "b1", "name": "Board",
						"columns": []map[string]interface{}{
							{
								"id": "status", "title": "Status", "type": "color",
								"settings_str": `{"labels":{"0":"Done","1":"Working on it","5":"Stuck"}}`,
							},
						},
						"items_page": map[string]interface{}{
							"cursor": nil,
							"items": []map[string]interface{}{
								{
									"id": "i1", "name": "Item 1",
									"column_values": []map[string]interface{}{
										{"id": "status", "text": "Done", "value": `{"index":0}`},
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

	// Status column should have all 3 configured options, not just "Done".
	var statusCol *struct{ Options []string }
	for _, c := range proj.Columns {
		if c.Name == "Status" {
			statusCol = &struct{ Options []string }{c.Options}
			break
		}
	}
	require.NotNil(t, statusCol, "Status column not found")
	assert.Equal(t, []string{"Done", "Working on it", "Stuck"}, statusCol.Options)
}

func TestMondayPeopleColumnResolvesEmails(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if strings.Contains(body["query"], "users(ids") {
			// User resolution query
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": map[string]interface{}{
					"users": []map[string]interface{}{
						{"id": "12345", "email": "alice@example.com"},
					},
				},
			})
			return
		}
		// Board query
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{
						"id": "b1", "name": "Board",
						"columns": []map[string]interface{}{
							{"id": "owner", "title": "Owner", "type": "people", "settings_str": ""},
						},
						"items_page": map[string]interface{}{
							"cursor": nil,
							"items": []map[string]interface{}{
								{
									"id": "i1", "name": "Task A",
									"column_values": []map[string]interface{}{
										{
											"id":    "owner",
											"text":  "",
											"value": `{"personsAndTeams":[{"id":12345,"kind":"person"}]}`,
										},
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
	require.Len(t, proj.Rows, 1)

	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	ownerVal, ok := cellMap["Owner"]
	require.True(t, ok, "Owner cell should be present")
	emails, ok := ownerVal.([]string)
	require.True(t, ok, "Owner value should be []string")
	assert.Equal(t, []string{"alice@example.com"}, emails)
}

func TestMondayTimelineColumnSplitsIntoTwo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{
						"id": "b1", "name": "Board",
						"columns": []map[string]interface{}{
							{"id": "timeline", "title": "Project Timeline", "type": "timeline", "settings_str": ""},
						},
						"items_page": map[string]interface{}{
							"cursor": nil,
							"items": []map[string]interface{}{
								{
									"id": "i1", "name": "Task A",
									"column_values": []map[string]interface{}{
										{
											"id":    "timeline",
											"text":  "Jan 15 - Jan 31",
											"value": `{"from":"2024-01-15","to":"2024-01-31"}`,
										},
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

	// Should have Name + "Project Timeline Start" + "Project Timeline End"
	colNames := make([]string, len(proj.Columns))
	for i, c := range proj.Columns {
		colNames[i] = c.Name
	}
	assert.Contains(t, colNames, "Project Timeline Start")
	assert.Contains(t, colNames, "Project Timeline End")
	assert.NotContains(t, colNames, "Project Timeline", "original timeline column should not exist")

	// Both date cells should be populated
	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, "2024-01-15", cellMap["Project Timeline Start"])
	assert.Equal(t, "2024-01-31", cellMap["Project Timeline End"])
}

func TestMondayFileColumnBecomesAttachment(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if strings.Contains(body["query"], "assets(ids") {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": map[string]interface{}{
					"assets": []map[string]interface{}{
						{"id": "99001", "name": "report.pdf", "public_url": "https://cdn.monday.com/report.pdf", "file_size": 204800},
					},
				},
			})
			return
		}
		// Board query
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": map[string]interface{}{
				"boards": []map[string]interface{}{
					{
						"id": "b1", "name": "Board",
						"columns": []map[string]interface{}{
							{"id": "files", "title": "Files", "type": "file", "settings_str": ""},
						},
						"items_page": map[string]interface{}{
							"cursor": nil,
							"items": []map[string]interface{}{
								{
									"id": "i1", "name": "Task A",
									"column_values": []map[string]interface{}{
										{
											"id":    "files",
											"text":  "",
											"value": `{"files":[{"assetId":99001,"name":"report.pdf"}]}`,
										},
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
	proj, err := e.ExtractProject(context.Background(), "", "b1", extractor.Options{IncludeAttachments: true})
	require.NoError(t, err)

	// No "Files" column should exist — files become attachments, not cells.
	for _, c := range proj.Columns {
		assert.NotEqual(t, "Files", c.Name, "file column should not appear as a sheet column")
	}

	require.Len(t, proj.Rows, 1)
	require.Len(t, proj.Rows[0].Attachments, 1)
	att := proj.Rows[0].Attachments[0]
	assert.Equal(t, "report.pdf", att.Name)
	assert.Equal(t, "https://cdn.monday.com/report.pdf", att.URL)
	assert.Equal(t, int64(204800), att.SizeBytes)
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
