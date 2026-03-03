package smartsheet_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/loader/smartsheet"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSheet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"resultCode": 0,
			"result": map[string]interface{}{
				"id":        float64(123456789),
				"name":      "Test Project",
				"permalink": "https://app.smartsheet.com/sheets/abc123",
				"columns": []map[string]interface{}{
					{"id": float64(11111), "title": "Name"},
					{"id": float64(22222), "title": "Status"},
				},
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
	sheetID, colMap, err := loader.CreateSheet(context.Background(), proj, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(123456789), sheetID)
	assert.Equal(t, int64(11111), colMap["Name"])
	assert.Equal(t, int64(22222), colMap["Status"])
}

func TestUploadAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/rows/")
		assert.Contains(t, r.URL.Path, "/attachments")
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"resultCode": 0}) //nolint:errcheck
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

func TestAddComment(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/rows/")
		assert.Contains(t, r.URL.Path, "/discussions")
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"resultCode": 0}) //nolint:errcheck
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	err := loader.AddComment(context.Background(), 123456789, 987654321, "This is a comment")
	require.NoError(t, err)
	assert.Contains(t, string(capturedBody), "This is a comment")
}

func TestBulkInsertRows(t *testing.T) {
	var receivedBatches [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rows") {
			body, _ := io.ReadAll(r.Body)
			receivedBatches = append(receivedBatches, body)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"resultCode": 0}) //nolint:errcheck
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))

	rows := make([]model.Row, 0, 600)
	for i := 0; i < 600; i++ {
		rows = append(rows, model.Row{
			ID:    fmt.Sprintf("row_%d", i),
			Cells: []model.Cell{{ColumnName: "Name", Value: fmt.Sprintf("Task %d", i)}},
		})
	}
	colMap := map[string]int64{"Name": 11111}

	err := loader.BulkInsertRows(context.Background(), 123456789, rows, colMap)
	require.NoError(t, err)
	assert.Len(t, receivedBatches, 2, "600 rows should produce 2 batches (500 + 100)")
}

func TestBulkInsertRowsEmpty(t *testing.T) {
	loader := smartsheet.New("fake-token")
	err := loader.BulkInsertRows(context.Background(), 123456789, []model.Row{}, map[string]int64{})
	require.NoError(t, err)
}

func TestCreateSheetAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	loader := smartsheet.New("bad-token", smartsheet.WithBaseURL(srv.URL))
	proj := &model.Project{
		Name:    "Test",
		Columns: []model.ColumnDef{{Name: "Name", Type: model.TypeText}},
	}
	_, _, err := loader.CreateSheet(context.Background(), proj, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestCreateSheetNonZeroResultCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"resultCode": 3,
			"result":     map[string]interface{}{"id": float64(0)},
		})
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	proj := &model.Project{
		Name:    "Test",
		Columns: []model.ColumnDef{{Name: "Name", Type: model.TypeText}},
	}
	_, _, err := loader.CreateSheet(context.Background(), proj, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resultCode=3")
}

func TestBulkInsertRowsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	rows := []model.Row{{ID: "r1", Cells: []model.Cell{{ColumnName: "Name", Value: "Task"}}}}
	err := loader.BulkInsertRows(context.Background(), 123456789, rows, map[string]int64{"Name": 111})
	assert.Error(t, err)
}
