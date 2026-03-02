package airtable_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	airtableext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/airtable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
