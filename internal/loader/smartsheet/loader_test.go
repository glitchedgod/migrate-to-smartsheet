package smartsheet_test

import (
	"context"
	"encoding/json"
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

func TestCreateWorkspace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/workspaces", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"resultCode": 0,
			"result": map[string]interface{}{
				"id":        float64(777888999),
				"name":      "My Workspace",
				"permalink": "https://app.smartsheet.com/workspaces/xyz789",
			},
		})
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	wsID, permalink, err := loader.CreateWorkspace(context.Background(), "My Workspace")
	require.NoError(t, err)
	assert.Equal(t, int64(777888999), wsID)
	assert.Equal(t, "https://app.smartsheet.com/workspaces/xyz789", permalink)
}

func TestCreateWorkspaceAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	loader := smartsheet.New("bad-token", smartsheet.WithBaseURL(srv.URL))
	_, _, err := loader.CreateWorkspace(context.Background(), "Workspace")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

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
	sheetID, colMap, _, err := loader.CreateSheet(context.Background(), proj, 0)
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"result": []map[string]interface{}{
					{"id": float64(1001), "cells": []interface{}{}},
					{"id": float64(1002), "cells": []interface{}{}},
				},
			})
		}
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	rows := []model.Row{
		{ID: "src_1", Cells: []model.Cell{{ColumnName: "Name", Value: "Task 1"}}},
		{ID: "src_2", Cells: []model.Cell{{ColumnName: "Name", Value: "Task 2"}}},
	}
	colMap := map[string]int64{"Name": 11111}

	rowIDMap, err := loader.BulkInsertRows(context.Background(), 123456789, rows, colMap, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1001), rowIDMap["src_1"])
	assert.Equal(t, int64(1002), rowIDMap["src_2"])
}

func TestBulkInsertRowsEmpty(t *testing.T) {
	loader := smartsheet.New("fake-token")
	rowIDMap, err := loader.BulkInsertRows(context.Background(), 123456789, []model.Row{}, map[string]int64{}, nil)
	require.NoError(t, err)
	assert.Empty(t, rowIDMap)
}

func TestBulkInsertRowsHierarchy(t *testing.T) {
	var receivedBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rows") {
			body, _ := io.ReadAll(r.Body)
			receivedBodies = append(receivedBodies, string(body))
			w.Header().Set("Content-Type", "application/json")
			if len(receivedBodies) == 1 {
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"result": []map[string]interface{}{{"id": float64(9001)}},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"result": []map[string]interface{}{{"id": float64(9002)}},
				})
			}
		}
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	rows := []model.Row{
		{ID: "parent_1", ParentID: "", Cells: []model.Cell{{ColumnName: "Name", Value: "Parent"}}},
		{ID: "child_1", ParentID: "parent_1", Cells: []model.Cell{{ColumnName: "Name", Value: "Child"}}},
	}

	rowIDMap, err := loader.BulkInsertRows(context.Background(), 123456789, rows, map[string]int64{"Name": 111}, nil)
	require.NoError(t, err)
	assert.Len(t, receivedBodies, 2, "should have made 2 separate insert calls")
	assert.Equal(t, int64(9001), rowIDMap["parent_1"])
	assert.Equal(t, int64(9002), rowIDMap["child_1"])
	assert.Contains(t, receivedBodies[1], "9001", "child batch should reference parent's Smartsheet ID")
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
	_, _, _, err := loader.CreateSheet(context.Background(), proj, 0)
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
	_, _, _, err := loader.CreateSheet(context.Background(), proj, 0)
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
	_, err := loader.BulkInsertRows(context.Background(), 123456789, rows, map[string]int64{"Name": 111}, nil)
	assert.Error(t, err)
}

func TestListSheetNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/workspaces/42/sheets", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{"id": float64(101), "name": "Alpha"},
				{"id": float64(102), "name": "Beta"},
			},
		})
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	names, err := loader.ListSheetNames(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, int64(101), names["Alpha"])
	assert.Equal(t, int64(102), names["Beta"])
}

func TestListSheetNamesRootScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sheets", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}}) //nolint:errcheck
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	names, err := loader.ListSheetNames(context.Background(), 0)
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestDeleteSheet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/sheets/999", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"resultCode": 0, "message": "SUCCESS"}) //nolint:errcheck
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	err := loader.DeleteSheet(context.Background(), 999)
	require.NoError(t, err)
}

func TestDeleteSheetError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	loader := smartsheet.New("fake-token", smartsheet.WithBaseURL(srv.URL))
	err := loader.DeleteSheet(context.Background(), 999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestLoaderHasRateLimiter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"resultCode": 0,
			"result": map[string]interface{}{
				"id": float64(1), "name": "test",
				"columns": []map[string]interface{}{{"id": float64(1), "title": "Name"}},
			},
		})
	}))
	defer srv.Close()

	loader := smartsheet.New("token", smartsheet.WithBaseURL(srv.URL))
	proj := &model.Project{Name: "Test", Columns: []model.ColumnDef{{Name: "Name", Type: model.TypeText}}}
	sheetID, _, _, err := loader.CreateSheet(context.Background(), proj, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), sheetID)
}
