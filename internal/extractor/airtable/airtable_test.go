package airtable_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	airtableext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/airtable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAirtableListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		if callCount == 1 {
			offset := "offset_abc"
			json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []map[string]interface{}{
					{"id": "rec_1", "fields": map[string]interface{}{"Name": "First"}},
				},
				"offset": &offset,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
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
	assert.Equal(t, 2, callCount, "should have made 2 API calls")
}

func TestAirtableExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
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
