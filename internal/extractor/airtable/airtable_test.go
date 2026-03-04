package airtable_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	airtableext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/airtable"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAirtableListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"bases": []map[string]interface{}{
				{"id": "base_1", "name": "My Base"},
				{"id": "base_2", "name": "Another Base"},
			},
		})
	}))
	defer srv.Close()

	e := airtableext.New("fake-token", airtableext.WithBaseURL(srv.URL))
	ws, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, ws, 2)
	assert.Equal(t, "base_1", ws[0].ID)
	assert.Equal(t, "My Base", ws[0].Name)
}

func TestAirtableExtractProjectPagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if strings.Contains(r.URL.Path, "/meta/") {
			// Schema call — return empty tables so pagination test keeps original behaviour
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"tables": []map[string]interface{}{},
			})
			return
		}
		if callCount == 2 {
			offset := "offset_abc"
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"records": []map[string]interface{}{
					{"id": "rec_1", "fields": map[string]interface{}{"Name": "First"}},
				},
				"offset": &offset,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"records": []map[string]interface{}{
					{"id": "rec_2", "fields": map[string]interface{}{"Name": "Second"}},
				},
			})
		}
	}))
	defer srv.Close()

	e := airtableext.New("fake-token", airtableext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "base_1", "tbl_1", extractor.Options{})
	require.NoError(t, err)
	assert.Len(t, proj.Rows, 2, "should have fetched both pages")
}

func TestAirtableExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/meta/") {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"tables": []map[string]interface{}{},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"records": []map[string]interface{}{
				{"id": "rec_1", "fields": map[string]interface{}{"Name": "First Record", "Status": "In Progress"}},
			},
		})
	}))
	defer srv.Close()

	e := airtableext.New("fake-token", airtableext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "base_1", "tbl_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "tbl_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
}

func TestAirtableListProjectsWithSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"tables": []map[string]interface{}{
				{
					"id": "tbl_1", "name": "Tasks",
					"fields": []map[string]interface{}{
						{"id": "fld_1", "name": "Name", "type": "singleLineText"},
						{"id": "fld_2", "name": "Status", "type": "singleSelect",
							"options": map[string]interface{}{
								"choices": []map[string]interface{}{{"name": "Todo"}, {"name": "Done"}},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	e := airtableext.New("fake-token", airtableext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "base_1")
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "tbl_1", projects[0].ID)
	assert.Equal(t, "Tasks", projects[0].Name)
}

func TestAirtableExtractProjectTypedColumns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/meta/") {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"tables": []map[string]interface{}{
					{
						"id": "tbl_1", "name": "Tasks",
						"fields": []map[string]interface{}{
							{"id": "fld_1", "name": "Name", "type": "singleLineText"},
							{"id": "fld_2", "name": "Done", "type": "checkbox"},
							{"id": "fld_3", "name": "Tags", "type": "multipleSelects"},
						},
					},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"records": []map[string]interface{}{
				{
					"id": "rec_1",
					"fields": map[string]interface{}{
						"Name": "Task 1",
						"Done": true,
						"Tags": []interface{}{"alpha", "beta"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	e := airtableext.New("fake-token", airtableext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "base_1", "tbl_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "Tasks", proj.Name)

	colTypeMap := make(map[string]model.ColumnType)
	for _, col := range proj.Columns {
		colTypeMap[col.Name] = col.Type
	}
	assert.Equal(t, model.TypeText, colTypeMap["Name"])
	assert.Equal(t, model.TypeCheckbox, colTypeMap["Done"])
	assert.Equal(t, model.TypeMultiSelect, colTypeMap["Tags"])

	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	tags, ok := cellMap["Tags"].([]string)
	assert.True(t, ok, "multipleSelects should be []string")
	assert.ElementsMatch(t, []string{"alpha", "beta"}, tags)
}
