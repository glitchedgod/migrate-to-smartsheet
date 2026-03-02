package smartsheet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/loader/smartsheet"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSheet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"resultCode": 0,
			"result": map[string]interface{}{
				"id":        float64(123456789),
				"name":      "Test Project",
				"permalink": "https://app.smartsheet.com/sheets/abc123",
			},
		})
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	proj := &model.Project{
		ID:   "proj1",
		Name: "Test Project",
		Columns: []model.ColumnDef{
			{Name: "Name", Type: model.TypeText},
			{Name: "Status", Type: model.TypeSingleSelect, Options: []string{"Todo", "Done"}},
		},
	}
	sheetID, err := loader.CreateSheet(context.Background(), proj, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(123456789), sheetID)
}

func TestUploadAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/rows/")
		assert.Contains(t, r.URL.Path, "/attachments")
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"resultCode": 0})
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	err := loader.UploadAttachment(
		context.Background(),
		123456789, // sheetID
		987654321, // rowID
		"test.txt",
		"text/plain",
		strings.NewReader("hello attachment"),
	)
	require.NoError(t, err)
}
