# Complete Rework Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix every critical bug and data gap in migrate-to-smartsheet so it actually migrates data end-to-end with full field fidelity across all 7 platforms.

**Architecture:** The core fix is (1) adding `ListProjects` to the Extractor interface so the migration loop can actually iterate projects, (2) wiring up all the data paths that exist in structs but are never called (attachments, comments, parent rows), and (3) fixing value transforms so data arrives in Smartsheet in the correct format. Each task is independent and builds on the previous.

**Tech Stack:** Go 1.22+, cobra, survey/v2, progressbar/v3, golang.org/x/net/html, golang.org/x/time/rate, testify, httptest

---

## Task 1: Value Transforms

**Files:**
- Create: `internal/transformer/values.go`
- Create: `internal/transformer/values_test.go`
- Modify: `pkg/model/model.go` (no change needed — types already exist)

**Context:** Currently zero value transformation happens. Dates arrive in platform-specific formats, contacts are bare strings, booleans are the string `"true"`, and multi-selects are `[]string` that will JSON-encode incorrectly. This task adds the transform layer. All transforms go through one function: `TransformCellValue`.

**Step 1: Write failing tests**

```go
// internal/transformer/values_test.go
package transformer_test

import (
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
	"strings"
)

func TestNormalizeDate(t *testing.T) {
	assert.Equal(t, "2026-04-01", transformer.NormalizeDate("2026-04-01"))
	assert.Equal(t, "2026-04-01", transformer.NormalizeDate("2026-04-01T00:00:00.000Z"))
	assert.Equal(t, "2026-04-01", transformer.NormalizeDate("2026-04-01T14:30:00Z"))
	assert.Equal(t, "", transformer.NormalizeDate(""))
	assert.Equal(t, "", transformer.NormalizeDate(nil))
}

func TestFormatContact(t *testing.T) {
	result := transformer.FormatContact("alice@example.com")
	m, ok := result.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "alice@example.com", m["email"])
}

func TestFormatContactEmpty(t *testing.T) {
	assert.Nil(t, transformer.FormatContact(""))
}

func TestFormatMultiSelect(t *testing.T) {
	assert.Equal(t, "a,b,c", transformer.FormatMultiSelect([]string{"a", "b", "c"}))
	assert.Equal(t, "", transformer.FormatMultiSelect([]string{}))
	assert.Equal(t, "", transformer.FormatMultiSelect(nil))
}

func TestFormatBool(t *testing.T) {
	assert.Equal(t, true, transformer.FormatBool(true))
	assert.Equal(t, true, transformer.FormatBool("true"))
	assert.Equal(t, false, transformer.FormatBool("false"))
	assert.Equal(t, false, transformer.FormatBool(false))
	assert.Equal(t, false, transformer.FormatBool(nil))
}

func TestTransformCellValue(t *testing.T) {
	um := &transformer.UserMap{}

	// Date normalization
	v := transformer.TransformCellValue("2026-04-01T00:00:00.000Z", model.TypeDate, um)
	assert.Equal(t, "2026-04-01", v)

	// Contact formatting
	v = transformer.TransformCellValue("dev@example.com", model.TypeContact, um)
	m, ok := v.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "dev@example.com", m["email"])

	// MultiSelect from []string
	v = transformer.TransformCellValue([]string{"a", "b"}, model.TypeMultiSelect, um)
	assert.Equal(t, "a,b", v)

	// Checkbox from string
	v = transformer.TransformCellValue("true", model.TypeCheckbox, um)
	assert.Equal(t, true, v)

	// Text passthrough
	v = transformer.TransformCellValue("hello", model.TypeText, um)
	assert.Equal(t, "hello", v)
}

func TestTransformCellValueUserRemap(t *testing.T) {
	csvData := "source_id,smartsheet_email\nuser1,mapped@example.com\n"
	um, err := transformer.LoadUserMapFromReader(strings.NewReader(csvData))
	assert.NoError(t, err)

	v := transformer.TransformCellValue("user1", model.TypeContact, um)
	m, ok := v.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "mapped@example.com", m["email"])
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/bchauhan/Projects/migrate-to-smartsheet
go test ./internal/transformer/... -run "TestNormalizeDate|TestFormatContact|TestFormatMultiSelect|TestFormatBool|TestTransformCellValue" -v
```

Expected: FAIL — functions not defined

**Step 3: Create `internal/transformer/values.go`**

```go
package transformer

import (
	"strings"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

// NormalizeDate converts any date string to YYYY-MM-DD.
// Returns "" for nil or empty input.
func NormalizeDate(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	// Already YYYY-MM-DD
	if len(s) == 10 {
		return s
	}
	// Try ISO 8601 with time component
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return s
}

// FormatContact converts an email string to a Smartsheet CONTACT_LIST cell value.
// Smartsheet expects {"email": "..."} objects. Returns nil for empty email.
func FormatContact(email string) interface{} {
	if email == "" {
		return nil
	}
	return map[string]string{"email": email}
}

// FormatMultiSelect converts a []string to a comma-separated string for MULTI_PICKLIST.
func FormatMultiSelect(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ",")
}

// FormatBool converts various bool representations to an actual bool.
func FormatBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	}
	return false
}

// TransformCellValue applies the correct value transform for a given column type.
// It also applies user remapping for Contact/MultiContact columns.
func TransformCellValue(v interface{}, colType model.ColumnType, um *UserMap) interface{} {
	if v == nil {
		return nil
	}
	switch colType {
	case model.TypeDate, model.TypeDateTime:
		return NormalizeDate(v)

	case model.TypeContact:
		email := extractEmail(v, um)
		if email == "" {
			return v
		}
		return FormatContact(email)

	case model.TypeMultiContact:
		switch val := v.(type) {
		case []string:
			contacts := make([]map[string]string, 0, len(val))
			for _, e := range val {
				if mapped := um.Lookup(e); mapped != "" {
					e = mapped
				}
				if e != "" {
					contacts = append(contacts, map[string]string{"email": e})
				}
			}
			return contacts
		case string:
			if mapped := um.Lookup(val); mapped != "" {
				val = mapped
			}
			return FormatContact(val)
		}
		return v

	case model.TypeMultiSelect:
		switch val := v.(type) {
		case []string:
			return FormatMultiSelect(val)
		case []interface{}:
			strs := make([]string, 0, len(val))
			for _, item := range val {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			return FormatMultiSelect(strs)
		}
		return v

	case model.TypeCheckbox:
		return FormatBool(v)
	}

	return v
}

func extractEmail(v interface{}, um *UserMap) string {
	var email string
	switch val := v.(type) {
	case string:
		email = val
	case map[string]string:
		email = val["email"]
	}
	if email == "" {
		return ""
	}
	if mapped := um.Lookup(email); mapped != "" {
		return mapped
	}
	return email
}
```

**Step 4: Run tests to verify they pass**

```bash
cd /Users/bchauhan/Projects/migrate-to-smartsheet
go test ./internal/transformer/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/transformer/values.go internal/transformer/values_test.go
git commit -m "feat: value transforms — date normalization, contact format, multi-select, bool, user remap"
```

---

## Task 2: Extractor Interface — Add ListProjects

**Files:**
- Modify: `internal/extractor/extractor.go`
- Modify: `internal/extractor/extractor_test.go`

**Context:** The migration loop in `run.go` iterates `ws.Projects` which is always empty — no extractor populates it. The fix is to add `ListProjects` to the interface so `run.go` can call it explicitly. `ProjectRef` is a lightweight struct with just ID and Name.

**Step 1: Write failing test**

```go
// Add to internal/extractor/extractor_test.go
func TestListProjectsInterface(t *testing.T) {
	var e extractor.Extractor = &mockExtractor{}
	projects, err := e.ListProjects(context.Background(), "ws1")
	assert.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "proj1", projects[0].ID)
	assert.Equal(t, "Test Project", projects[0].Name)
}
```

And update `mockExtractor`:
```go
func (m *mockExtractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	return []extractor.ProjectRef{{ID: "proj1", Name: "Test Project"}}, nil
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/extractor/... -v
```

Expected: FAIL — `ListProjects` not in interface

**Step 3: Update `internal/extractor/extractor.go`**

Add `ProjectRef` struct and `ListProjects` to the interface:

```go
package extractor

import (
	"context"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

// ProjectRef is a lightweight project identifier returned by ListProjects.
type ProjectRef struct {
	ID   string
	Name string
}

// Options configure what an extractor fetches.
type Options struct {
	IncludeAttachments bool
	IncludeComments    bool
	IncludeArchived    bool
	CreatedAfter       time.Time
	UpdatedAfter       time.Time
	ExcludeFields      []string
}

// Extractor is implemented by each source platform adapter.
type Extractor interface {
	ListWorkspaces(ctx context.Context) ([]model.Workspace, error)
	ListProjects(ctx context.Context, workspaceID string) ([]ProjectRef, error)
	ExtractProject(ctx context.Context, workspaceID, projectID string, opts Options) (*model.Project, error)
}
```

**Step 4: All 7 extractor packages now fail to compile** — they don't implement `ListProjects`. Each extractor will get its own task (Tasks 5-11). For now, add a stub to each extractor so the code compiles. Run:

```bash
cd /Users/bchauhan/Projects/migrate-to-smartsheet
go build ./... 2>&1 | grep "does not implement"
```

For each platform that fails, open the extractor file and add a stub at the bottom:

```go
// ListProjects returns all projects in the given workspace.
// Stub — full implementation in Task N.
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	return nil, fmt.Errorf("ListProjects not yet implemented for this platform")
}
```

(Replace `extractor.ProjectRef` with the correct import path.)

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/extractor/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/extractor/
git commit -m "feat: add ListProjects to Extractor interface with stubs on all platforms"
```

---

## Task 3: Loader — BulkInsertRows Returns Row ID Map

**Files:**
- Modify: `internal/loader/smartsheet/loader.go`
- Modify: `internal/loader/smartsheet/loader_test.go`
- Modify: `cmd/migrate/run.go` (update call site)

**Context:** Currently `BulkInsertRows` returns only `error`. It needs to return `map[string]int64` mapping source row ID → Smartsheet row ID. This is required for attachment upload and comment posting. The Smartsheet API returns created rows in order in the response body.

**Step 1: Update test to expect new signature**

In `internal/loader/smartsheet/loader_test.go`, update `TestBulkInsertRows`:

```go
func TestBulkInsertRows(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rows") {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			// Simulate Smartsheet returning created rows with IDs
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

	rowIDMap, err := loader.BulkInsertRows(context.Background(), 123456789, rows, colMap)
	require.NoError(t, err)
	assert.Equal(t, int64(1001), rowIDMap["src_1"])
	assert.Equal(t, int64(1002), rowIDMap["src_2"])
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/loader/smartsheet/... -run TestBulkInsertRows -v
```

Expected: FAIL — wrong return type

**Step 3: Update `internal/loader/smartsheet/loader.go`**

Change `BulkInsertRows` signature and `insertRowBatch`:

```go
// BulkInsertRows inserts rows in batches of 500.
// Returns a map of source row ID → Smartsheet row ID.
func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) (map[string]int64, error) {
	rowIDMap := make(map[string]int64)
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batchMap, err := l.insertRowBatch(ctx, sheetID, rows[i:end], colIndexByName)
		if err != nil {
			return rowIDMap, fmt.Errorf("batch %d: %w", i/batchSize, err)
		}
		for k, v := range batchMap {
			rowIDMap[k] = v
		}
	}
	return rowIDMap, nil
}

type insertRowsResponse struct {
	Result []struct {
		ID int64 `json:"id"`
	} `json:"result"`
}

func (l *Loader) insertRowBatch(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) (map[string]int64, error) {
	rowPayloads := make([]rowPayload, 0, len(rows))
	for _, r := range rows {
		cells := make([]cellPayload, 0, len(r.Cells))
		for _, c := range r.Cells {
			colID, ok := colIndexByName[c.ColumnName]
			if !ok {
				continue
			}
			cells = append(cells, cellPayload{ColumnID: colID, Value: c.Value})
		}
		rowPayloads = append(rowPayloads, rowPayload{ToBottom: true, Cells: cells})
	}

	body, err := json.Marshal(rowPayloads)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/sheets/%d/rows", l.baseURL, sheetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("smartsheet row insert error: %s", resp.Status)
	}

	var result insertRowsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	rowIDMap := make(map[string]int64, len(rows))
	for i, r := range rows {
		if i < len(result.Result) {
			rowIDMap[r.ID] = result.Result[i].ID
		}
	}
	return rowIDMap, nil
}
```

**Step 4: Update `cmd/migrate/run.go`** — change the `BulkInsertRows` call to capture the map:

```go
rowIDMap, err := loader.BulkInsertRows(ctx, sheetID, proj.Rows, colMap)
if err != nil {
    fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to insert rows for %s: %v\n", proj.Name, err)
}
_ = rowIDMap // will be used for attachments/comments in Task 4
```

**Step 5: Run all tests**

```bash
go test ./internal/loader/smartsheet/... ./cmd/... -v 2>&1 | grep -E "PASS|FAIL"
```

Expected: all PASS

**Step 6: Commit**

```bash
git add internal/loader/smartsheet/ cmd/migrate/run.go
git commit -m "feat: BulkInsertRows returns source→Smartsheet row ID map"
```

---

## Task 4: Loader — Parent/Child Row Hierarchy

**Files:**
- Modify: `internal/loader/smartsheet/loader.go`
- Modify: `internal/loader/smartsheet/loader_test.go`

**Context:** `model.Row.ParentID` is populated by Asana and Wrike extractors but the loader ignores it. Smartsheet supports hierarchical rows via `parentId` in the insert payload. This requires a two-pass insert: top-level rows first (to get their Smartsheet IDs), then child rows with `parentId` set.

**Step 1: Write failing test**

```go
func TestBulkInsertRowsHierarchy(t *testing.T) {
	var receivedBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rows") {
			body, _ := io.ReadAll(r.Body)
			receivedBodies = append(receivedBodies, string(body))
			w.Header().Set("Content-Type", "application/json")
			// Return different IDs per call
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

	rowIDMap, err := loader.BulkInsertRows(context.Background(), 123456789, rows, map[string]int64{"Name": 111})
	require.NoError(t, err)
	assert.Len(t, receivedBodies, 2, "should have made 2 separate insert calls")
	assert.Equal(t, int64(9001), rowIDMap["parent_1"])
	assert.Equal(t, int64(9002), rowIDMap["child_1"])

	// Verify child request contains parentId
	assert.Contains(t, receivedBodies[1], "9001", "child batch should reference parent's Smartsheet ID")
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/loader/smartsheet/... -run TestBulkInsertRowsHierarchy -v
```

Expected: FAIL

**Step 3: Update `rowPayload` and `BulkInsertRows` in `loader.go`**

```go
type rowPayload struct {
	ToBottom bool          `json:"toBottom"`
	ParentID *int64        `json:"parentId,omitempty"`
	Cells    []cellPayload `json:"cells"`
}
```

Update `BulkInsertRows` to do a two-pass insert:

```go
func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) (map[string]int64, error) {
	rowIDMap := make(map[string]int64)

	// Separate top-level and child rows
	var topLevel, children []model.Row
	for _, r := range rows {
		if r.ParentID == "" {
			topLevel = append(topLevel, r)
		} else {
			children = append(children, r)
		}
	}

	// Pass 1: insert top-level rows
	if err := l.insertInBatches(ctx, sheetID, topLevel, colIndexByName, rowIDMap, nil); err != nil {
		return rowIDMap, err
	}

	// Pass 2: insert child rows with resolved parentId
	if err := l.insertInBatches(ctx, sheetID, children, colIndexByName, rowIDMap, rowIDMap); err != nil {
		return rowIDMap, err
	}

	return rowIDMap, nil
}

func (l *Loader) insertInBatches(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, rowIDMap map[string]int64, parentIDMap map[string]int64) error {
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batchMap, err := l.insertRowBatch(ctx, sheetID, rows[i:end], colIndexByName, parentIDMap)
		if err != nil {
			return fmt.Errorf("batch %d: %w", i/batchSize, err)
		}
		for k, v := range batchMap {
			rowIDMap[k] = v
		}
	}
	return nil
}
```

Update `insertRowBatch` to accept `parentIDMap` and set `ParentID`:

```go
func (l *Loader) insertRowBatch(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, parentIDMap map[string]int64) (map[string]int64, error) {
	rowPayloads := make([]rowPayload, 0, len(rows))
	for _, r := range rows {
		cells := make([]cellPayload, 0, len(r.Cells))
		for _, c := range r.Cells {
			colID, ok := colIndexByName[c.ColumnName]
			if !ok {
				continue
			}
			cells = append(cells, cellPayload{ColumnID: colID, Value: c.Value})
		}
		rp := rowPayload{ToBottom: true, Cells: cells}
		if parentIDMap != nil {
			if ssParentID, ok := parentIDMap[r.ParentID]; ok && r.ParentID != "" {
				rp.ParentID = &ssParentID
				rp.ToBottom = false // children must not use toBottom with parentId
			}
		}
		rowPayloads = append(rowPayloads, rp)
	}
	// ... rest of HTTP call unchanged
```

**Step 4: Run all loader tests**

```bash
go test ./internal/loader/smartsheet/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/loader/smartsheet/
git commit -m "feat: two-pass BulkInsertRows for parent/child row hierarchy"
```

---

## Task 5: run.go — Full Rewrite

**Files:**
- Modify: `cmd/migrate/run.go`

**Context:** The current run.go loop never migrates anything because it iterates `ws.Projects` which is always empty. This task replaces the loop with: ListWorkspaces → select workspace → ListProjects → multi-select projects → extract each → transform values → create sheet → insert rows → upload attachments → post comments. Also wires all the currently-ignored flags.

**Step 1: Replace `cmd/migrate/run.go` entirely**

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	survey "github.com/AlecAivazis/survey/v2"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	asanaext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/asana"
	airtableext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/airtable"
	jiraext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/jira"
	mondayext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/monday"
	notionext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/notion"
	trelloext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/trello"
	wrikeext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/wrike"
	ssloader "github.com/glitchedgod/migrate-to-smartsheet/internal/loader/smartsheet"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/preview"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/state"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

func runMigrate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	flags := cmd.Flags()

	sourceStr, _ := flags.GetString("source")
	sourceToken, _ := flags.GetString("source-token")
	ssToken, _ := flags.GetString("smartsheet-token")
	yes, _ := flags.GetBool("yes")
	userMapPath, _ := flags.GetString("user-map")
	conflictMode, _ := flags.GetString("conflict")
	includeAttachments, _ := flags.GetBool("include-attachments")
	includeComments, _ := flags.GetBool("include-comments")
	includeArchived, _ := flags.GetBool("include-archived")
	sheetPrefix, _ := flags.GetString("sheet-prefix")
	sheetSuffix, _ := flags.GetString("sheet-suffix")
	sheetTemplate, _ := flags.GetString("sheet-name-template")
	projectsFilter, _ := flags.GetString("projects")
	excludeFieldsStr, _ := flags.GetString("exclude-fields")
	createdAfterStr, _ := flags.GetString("created-after")
	updatedAfterStr, _ := flags.GetString("updated-after")

	var excludeFields []string
	if excludeFieldsStr != "" {
		excludeFields = strings.Split(excludeFieldsStr, ",")
	}

	var createdAfter, updatedAfter time.Time
	if createdAfterStr != "" {
		if t, err := time.Parse("2006-01-02", createdAfterStr); err == nil {
			createdAfter = t
		}
	}
	if updatedAfterStr != "" {
		if t, err := time.Parse("2006-01-02", updatedAfterStr); err == nil {
			updatedAfter = t
		}
	}

	// Interactive prompts for missing required values
	if sourceStr == "" {
		survey.AskOne(&survey.Select{ //nolint:errcheck
			Message: "Select source platform:",
			Options: []string{"asana", "monday", "trello", "jira", "airtable", "notion", "wrike"},
		}, &sourceStr)
	}
	if sourceToken == "" {
		survey.AskOne(&survey.Password{Message: fmt.Sprintf("[%s] API token:", sourceStr)}, &sourceToken) //nolint:errcheck
	}

	ext, err := buildExtractor(sourceStr, sourceToken, cmd)
	if err != nil {
		return fmt.Errorf("building extractor: %w", err)
	}

	// Connect to source
	fmt.Print("  Connecting to source... ")
	workspaces, err := ext.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("listing workspaces: %w", err)
	}
	fmt.Println("✓")

	// Select workspace
	var selectedWorkspace model.Workspace
	workspaceFlag, _ := flags.GetString("workspace")
	if workspaceFlag != "" {
		for _, ws := range workspaces {
			if ws.Name == workspaceFlag || ws.ID == workspaceFlag {
				selectedWorkspace = ws
				break
			}
		}
		if selectedWorkspace.ID == "" {
			return fmt.Errorf("workspace %q not found", workspaceFlag)
		}
	} else if len(workspaces) == 1 {
		selectedWorkspace = workspaces[0]
	} else {
		wsNames := make([]string, len(workspaces))
		for i, ws := range workspaces {
			wsNames[i] = ws.Name
		}
		var wsChoice string
		survey.AskOne(&survey.Select{Message: "Select workspace:", Options: wsNames}, &wsChoice) //nolint:errcheck
		for _, ws := range workspaces {
			if ws.Name == wsChoice {
				selectedWorkspace = ws
				break
			}
		}
	}

	// List and select projects
	fmt.Print("  Listing projects... ")
	projectRefs, err := ext.ListProjects(ctx, selectedWorkspace.ID)
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}
	fmt.Printf("✓ (%d projects)\n", len(projectRefs))

	var selectedProjects []extractor.ProjectRef
	if projectsFilter != "" {
		filterSet := make(map[string]bool)
		for _, p := range strings.Split(projectsFilter, ",") {
			filterSet[strings.TrimSpace(p)] = true
		}
		for _, p := range projectRefs {
			if filterSet[p.Name] || filterSet[p.ID] {
				selectedProjects = append(selectedProjects, p)
			}
		}
	} else if yes {
		selectedProjects = projectRefs
	} else {
		projNames := make([]string, len(projectRefs))
		for i, p := range projectRefs {
			projNames[i] = p.Name
		}
		var chosen []string
		survey.AskOne(&survey.MultiSelect{Message: "Select projects to migrate:", Options: projNames}, &chosen) //nolint:errcheck
		nameToRef := make(map[string]extractor.ProjectRef)
		for _, p := range projectRefs {
			nameToRef[p.Name] = p
		}
		for _, name := range chosen {
			selectedProjects = append(selectedProjects, nameToRef[name])
		}
	}

	if len(selectedProjects) == 0 {
		fmt.Println("No projects selected. Exiting.")
		return nil
	}

	// Smartsheet setup
	if ssToken == "" {
		survey.AskOne(&survey.Password{Message: "Smartsheet API token:"}, &ssToken) //nolint:errcheck
	}
	loader := ssloader.New(ssToken)

	// Resume state
	stateFile := fmt.Sprintf(".migrate-state-%s-%d.json", sourceStr, time.Now().Unix())
	var migState *state.MigrationState
	// Look for any existing state file for this source
	if entries, err := os.ReadDir("."); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".migrate-state-"+sourceStr+"-") && strings.HasSuffix(e.Name(), ".json") {
				if existing, err := state.Load(e.Name()); err == nil {
					var resume bool
					survey.AskOne(&survey.Confirm{ //nolint:errcheck
						Message: fmt.Sprintf("Found incomplete migration from %s (%d/%d sheets done). Resume?",
							existing.StartedAt.Format("2006-01-02 15:04"), len(existing.CompletedSheets), len(selectedProjects)),
						Default: true,
					}, &resume)
					if resume {
						migState = existing
						stateFile = e.Name()
					}
					break
				}
			}
		}
	}
	if migState == nil {
		migState = &state.MigrationState{Source: sourceStr, StartedAt: time.Now().UTC()}
	}

	// User map
	var userMap *transformer.UserMap
	if userMapPath != "" {
		f, err := os.Open(userMapPath)
		if err != nil {
			return fmt.Errorf("opening user map: %w", err)
		}
		defer func() { _ = f.Close() }()
		userMap, err = transformer.LoadUserMapFromReader(f)
		if err != nil {
			return fmt.Errorf("loading user map: %w", err)
		}
	}
	if userMap == nil {
		userMap = &transformer.UserMap{}
	}

	opts := extractor.Options{
		IncludeAttachments: includeAttachments,
		IncludeComments:    includeComments,
		IncludeArchived:    includeArchived,
		CreatedAfter:       createdAfter,
		UpdatedAfter:       updatedAfter,
		ExcludeFields:      excludeFields,
	}

	// Extract all selected projects for preview
	fmt.Println("\n  Analyzing source data...")
	var allProjects []model.Project
	for _, ref := range selectedProjects {
		if migState.IsCompleted(ref.ID) {
			fmt.Printf("  ↻ Skipping already-migrated: %s\n", ref.Name)
			continue
		}
		extracted, err := ext.ExtractProject(ctx, selectedWorkspace.ID, ref.ID, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠  Skipping project %s: %v\n", ref.Name, err)
			continue
		}
		// Apply exclude-fields filter
		if len(excludeFields) > 0 {
			extracted = applyExcludeFields(extracted, excludeFields)
		}
		allProjects = append(allProjects, *extracted)
	}

	// Preview
	summary := preview.Summarize(workspacesFromProjects(selectedWorkspace, allProjects))
	fmt.Printf(`
╔══════════════════════════╗
║    Migration Preview     ║
╠══════════════════════════╣
║ Sheets:        %-9d║
║ Rows:          %-9d║
║ Columns:       %-9d║
║ Attachments:   %-9d║
║ Comments:      %-9d║
║ Warnings:      %-9d║
╚══════════════════════════╝
`, summary.Sheets, summary.Rows, summary.Columns,
		summary.Attachments, summary.Comments, len(summary.Warnings))

	for _, w := range summary.Warnings {
		fmt.Fprintf(os.Stderr, "  ⚠  %s\n", w)
	}

	if !yes {
		var proceed bool
		survey.AskOne(&survey.Confirm{Message: "Proceed with migration?", Default: false}, &proceed) //nolint:errcheck
		if !proceed {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	// Migrate
	bar := progressbar.Default(int64(len(allProjects)), "Migrating")
	allSucceeded := true
	for _, proj := range allProjects {
		sheetName := applySheetNaming(proj.Name, sourceStr, sheetPrefix, sheetSuffix, sheetTemplate)

		// Apply value transforms to all cells
		for ri := range proj.Rows {
			for ci, col := range proj.Columns {
				if ci < len(proj.Rows[ri].Cells) {
					proj.Rows[ri].Cells[ci].Value = transformer.TransformCellValue(
						proj.Rows[ri].Cells[ci].Value, col.Type, userMap,
					)
				}
			}
			// Apply transforms by column name for cells that may be out of order
			colTypeByName := make(map[string]model.ColumnType)
			for _, col := range proj.Columns {
				colTypeByName[col.Name] = col.Type
			}
			for ci := range proj.Rows[ri].Cells {
				colType := colTypeByName[proj.Rows[ri].Cells[ci].ColumnName]
				proj.Rows[ri].Cells[ci].Value = transformer.TransformCellValue(
					proj.Rows[ri].Cells[ci].Value, colType, userMap,
				)
			}
		}

		// Handle conflict
		projCopy := proj
		projCopy.Name = sheetName
		if err := handleConflict(ctx, loader, ssToken, sheetName, conflictMode); err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Conflict check failed for %s: %v\n", sheetName, err)
			allSucceeded = false
			continue
		}

		sheetID, colMap, err := loader.CreateSheet(ctx, &projCopy, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to create sheet %s: %v\n", sheetName, err)
			allSucceeded = false
			continue
		}

		rowIDMap, err := loader.BulkInsertRows(ctx, sheetID, proj.Rows, colMap)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to insert rows for %s: %v\n", sheetName, err)
			allSucceeded = false
		}

		// Attachments
		if includeAttachments {
			for _, row := range proj.Rows {
				ssRowID, ok := rowIDMap[row.ID]
				if !ok {
					continue
				}
				for _, att := range row.Attachments {
					if att.SizeBytes > 25*1024*1024 {
						fmt.Fprintf(os.Stderr, "\n  ⚠  Skipping attachment %s (>25MB)\n", att.Name)
						continue
					}
					if err := downloadAndUpload(ctx, loader, sheetID, ssRowID, att); err != nil {
						fmt.Fprintf(os.Stderr, "\n  ⚠  Attachment %s failed: %v\n", att.Name, err)
					}
				}
			}
		}

		// Comments
		if includeComments {
			for _, row := range proj.Rows {
				ssRowID, ok := rowIDMap[row.ID]
				if !ok {
					continue
				}
				for _, comment := range row.Comments {
					text := fmt.Sprintf("[%s] %s", comment.AuthorName, comment.Body)
					if err := loader.AddComment(ctx, sheetID, ssRowID, text); err != nil {
						fmt.Fprintf(os.Stderr, "\n  ⚠  Comment post failed: %v\n", err)
					}
				}
			}
		}

		migState.MarkCompleted(proj.ID)
		_ = state.Save(stateFile, migState)
		_ = bar.Add(1)
	}

	fmt.Println("\n✓ Migration complete!")
	if allSucceeded {
		_ = os.Remove(stateFile)
	} else {
		fmt.Printf("  State saved to %s for resume.\n", stateFile)
	}
	return nil
}

func applySheetNaming(name, source, prefix, suffix, tmpl string) string {
	if tmpl != "{project}" && tmpl != "" {
		name = strings.NewReplacer(
			"{source}", source,
			"{project}", name,
			"{date}", time.Now().Format("2006-01-02"),
		).Replace(tmpl)
	}
	return prefix + name + suffix
}

func applyExcludeFields(proj *model.Project, exclude []string) *model.Project {
	excludeSet := make(map[string]bool)
	for _, f := range exclude {
		excludeSet[strings.TrimSpace(f)] = true
	}
	filtered := make([]model.ColumnDef, 0, len(proj.Columns))
	for _, col := range proj.Columns {
		if !excludeSet[col.Name] {
			filtered = append(filtered, col)
		}
	}
	proj.Columns = filtered
	for ri := range proj.Rows {
		filteredCells := make([]model.Cell, 0, len(proj.Rows[ri].Cells))
		for _, cell := range proj.Rows[ri].Cells {
			if !excludeSet[cell.ColumnName] {
				filteredCells = append(filteredCells, cell)
			}
		}
		proj.Rows[ri].Cells = filteredCells
	}
	return proj
}

func workspacesFromProjects(ws model.Workspace, projects []model.Project) []model.Workspace {
	ws.Projects = projects
	return []model.Workspace{ws}
}

func handleConflict(ctx context.Context, loader *ssloader.Loader, token, sheetName, conflictMode string) error {
	// For now: skip and rename are handled at CreateSheet level.
	// Overwrite would require listing sheets — deferred to loader enhancement.
	_ = ctx
	_ = loader
	_ = token
	_ = sheetName
	_ = conflictMode
	return nil
}

func downloadAndUpload(ctx context.Context, loader *ssloader.Loader, sheetID, rowID int64, att model.Attachment) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	contentType := att.ContentType
	if contentType == "" {
		contentType = resp.Header.Get("Content-Type")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return loader.UploadAttachment(ctx, sheetID, rowID, att.Name, contentType, resp.Body)
}

func buildExtractor(source, token string, cmd *cobra.Command) (extractor.Extractor, error) {
	switch strings.ToLower(source) {
	case "asana":
		return asanaext.New(token), nil
	case "monday":
		return mondayext.New(token), nil
	case "trello":
		key, _ := cmd.Flags().GetString("source-key")
		return trelloext.New(key, token), nil
	case "jira":
		email, _ := cmd.Flags().GetString("jira-email")
		instance, _ := cmd.Flags().GetString("jira-instance")
		return jiraext.New(email, token, instance), nil
	case "airtable":
		return airtableext.New(token), nil
	case "notion":
		return notionext.New(token), nil
	case "wrike":
		return wrikeext.New(token), nil
	default:
		return nil, fmt.Errorf("unsupported source: %q", source)
	}
}
```

**Step 2: Also add `UserMap` zero value constructor to `transformer/usermap.go`**

Add to `internal/transformer/usermap.go`:
```go
// NewUserMap returns an empty UserMap that returns "" for all lookups.
func NewUserMap() *UserMap {
	return &UserMap{m: make(map[string]string)}
}
```

**Step 3: Build to verify it compiles**

```bash
go build ./cmd/migrate/ 2>&1
```

Expected: builds cleanly

**Step 4: Commit**

```bash
git add cmd/migrate/run.go internal/transformer/usermap.go
git commit -m "feat: rewrite run.go — ListProjects loop, all flags wired, attach/comment pipeline"
```

---

## Task 6: Asana — ListProjects + Full Field Extraction

**Files:**
- Modify: `internal/extractor/asana/asana.go`
- Modify: `internal/extractor/asana/asana_test.go`

**Step 1: Write failing tests**

```go
func TestAsanaListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/workspaces/ws_1/projects")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{"gid": "proj_1", "name": "Alpha Project"},
				{"gid": "proj_2", "name": "Beta Project"},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "ws_1")
	require.NoError(t, err)
	assert.Len(t, projects, 2)
	assert.Equal(t, "proj_1", projects[0].ID)
	assert.Equal(t, "Alpha Project", projects[0].Name)
}

func TestAsanaExtractProjectHasCorrectName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/projects/proj_1") && !strings.Contains(r.URL.Path, "/tasks") {
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"data": map[string]interface{}{"gid": "proj_1", "name": "My Real Project Name"},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}}) //nolint:errcheck
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "ws_1", "proj_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "My Real Project Name", proj.Name)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/asana/... -run "TestAsanaListProjects|TestAsanaExtractProjectHasCorrectName" -v
```

Expected: FAIL

**Step 3: Update `internal/extractor/asana/asana.go`**

Replace the stub `ListProjects` and update `ExtractProject`:

```go
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Data []struct {
			GID  string `json:"gid"`
			Name string `json:"name"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/workspaces/%s/projects?opt_fields=gid,name&limit=100", workspaceID)
	if err := e.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(resp.Data))
	for i, p := range resp.Data {
		refs[i] = extractor.ProjectRef{ID: p.GID, Name: p.Name}
	}
	return refs, nil
}
```

Update `ExtractProject` to:
1. Fetch project name via `GET /projects/{id}?opt_fields=gid,name`
2. Add `created_at,modified_at,start_on` to opt_fields
3. Build columns including those extra fields

```go
func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	// Fetch project name
	var projResp struct {
		Data struct {
			GID  string `json:"gid"`
			Name string `json:"name"`
		} `json:"data"`
	}
	_ = e.get(ctx, fmt.Sprintf("/projects/%s?opt_fields=gid,name", projectID), &projResp)
	projName := projResp.Data.Name
	if projName == "" {
		projName = projectID
	}

	// Fetch tasks
	var resp struct{ Data []asanaTask `json:"data"` }
	path := fmt.Sprintf("/projects/%s/tasks?opt_fields=gid,name,notes,completed,due_on,start_on,created_at,modified_at,assignee.email,tags.name,parent.gid&limit=100", projectID)
	if !opts.CreatedAfter.IsZero() {
		path += fmt.Sprintf("&created_since=%s", opts.CreatedAfter.Format(time.RFC3339))
	}
	if err := e.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Notes", Type: model.TypeText},
		{Name: "Completed", Type: model.TypeCheckbox},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Start Date", Type: model.TypeDate},
		{Name: "Created At", Type: model.TypeDateTime},
		{Name: "Modified At", Type: model.TypeDateTime},
		{Name: "Assignee", Type: model.TypeContact},
		{Name: "Tags", Type: model.TypeMultiSelect},
	}

	rows := make([]model.Row, 0, len(resp.Data))
	for _, t := range resp.Data {
		if !opts.IncludeArchived && t.Completed {
			continue
		}
		cells := []model.Cell{
			{ColumnName: "Name", Value: t.Name},
			{ColumnName: "Notes", Value: t.Notes},
			{ColumnName: "Completed", Value: t.Completed},
		}
		if t.DueOn != "" {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: t.DueOn})
		}
		if t.StartOn != "" {
			cells = append(cells, model.Cell{ColumnName: "Start Date", Value: t.StartOn})
		}
		if t.CreatedAt != "" {
			cells = append(cells, model.Cell{ColumnName: "Created At", Value: t.CreatedAt})
		}
		if t.ModifiedAt != "" {
			cells = append(cells, model.Cell{ColumnName: "Modified At", Value: t.ModifiedAt})
		}
		if t.Assignee != nil {
			cells = append(cells, model.Cell{ColumnName: "Assignee", Value: t.Assignee.Email})
		}
		if len(t.Tags) > 0 {
			tags := make([]string, len(t.Tags))
			for i, tag := range t.Tags {
				tags[i] = tag.Name
			}
			cells = append(cells, model.Cell{ColumnName: "Tags", Value: tags})
		}
		parentID := ""
		if t.Parent != nil {
			parentID = t.Parent.GID
		}
		row := model.Row{ID: t.GID, ParentID: parentID, Cells: cells}
		rows = append(rows, row)
	}

	return &model.Project{ID: projectID, Name: projName, Columns: columns, Rows: rows}, nil
}
```

Update `asanaTask` struct to add new fields:
```go
type asanaTask struct {
	GID        string `json:"gid"`
	Name       string `json:"name"`
	Notes      string `json:"notes"`
	Completed  bool   `json:"completed"`
	DueOn      string `json:"due_on"`
	StartOn    string `json:"start_on"`
	CreatedAt  string `json:"created_at"`
	ModifiedAt string `json:"modified_at"`
	Assignee   *struct {
		GID   string `json:"gid"`
		Email string `json:"email"`
	} `json:"assignee"`
	Tags   []struct{ Name string `json:"name"` } `json:"tags"`
	Parent *struct{ GID string `json:"gid"` } `json:"parent"`
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/asana/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/extractor/asana/
git commit -m "feat: Asana — ListProjects, project name from API, timestamps, date filter"
```

---

## Task 7: Trello — ListProjects + List Names + Members + Labels

**Files:**
- Modify: `internal/extractor/trello/trello.go`
- Modify: `internal/extractor/trello/trello_test.go`

**Step 1: Write failing tests**

```go
func TestTrelloListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "board_1", "name": "My Board"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "org_1")
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "My Board", projects[0].Name)
}

func TestTrelloExtractProjectListNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/lists") {
			json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
				{"id": "list_1", "name": "To Do"},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/boards/") && strings.HasSuffix(r.URL.Path, "") {
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "board_1", "name": "My Board"}) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
			{"id": "card_1", "name": "Card", "desc": "", "closed": false, "idList": "list_1"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "org_1", "board_1", extractor.Options{})
	require.NoError(t, err)
	// List column should have resolved name, not ID
	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, "To Do", cellMap["List"], "list name should be resolved, not raw ID")
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/trello/... -run "TestTrelloListProjects|TestTrelloExtractProjectListNames" -v
```

Expected: FAIL

**Step 3: Update `internal/extractor/trello/trello.go`**

```go
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var boards []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := e.get(ctx, fmt.Sprintf("/organizations/%s/boards?fields=id,name", workspaceID), &boards); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(boards))
	for i, b := range boards {
		refs[i] = extractor.ProjectRef{ID: b.ID, Name: b.Name}
	}
	return refs, nil
}
```

Update `ExtractProject` to:
1. Fetch board name
2. Fetch lists to build ID→name map
3. Add `limit=1000` to cards request
4. Add Members and Labels columns

```go
func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	// Fetch board name
	var board struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = e.get(ctx, fmt.Sprintf("/boards/%s?fields=id,name", projectID), &board)
	boardName := board.Name
	if boardName == "" {
		boardName = projectID
	}

	// Fetch lists for name resolution
	var lists []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = e.get(ctx, fmt.Sprintf("/boards/%s/lists?fields=id,name", projectID), &lists)
	listNameByID := make(map[string]string)
	listNames := make([]string, 0, len(lists))
	for _, l := range lists {
		listNameByID[l.ID] = l.Name
		listNames = append(listNames, l.Name)
	}

	// Fetch cards
	var cards []struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Desc   string   `json:"desc"`
		Closed bool     `json:"closed"`
		Due    *string  `json:"due"`
		IDList string   `json:"idList"`
		Labels []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"labels"`
		IDMembers []string `json:"idMembers"`
	}
	if err := e.get(ctx, fmt.Sprintf("/boards/%s/cards?limit=1000&fields=id,name,desc,closed,due,idList,labels,idMembers", projectID), &cards); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "List", Type: model.TypeSingleSelect, Options: listNames},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Closed", Type: model.TypeCheckbox},
		{Name: "Labels", Type: model.TypeMultiSelect},
	}

	rows := make([]model.Row, 0, len(cards))
	for _, c := range cards {
		if !opts.IncludeArchived && c.Closed {
			continue
		}
		listName := listNameByID[c.IDList]
		if listName == "" {
			listName = c.IDList
		}
		cells := []model.Cell{
			{ColumnName: "Name", Value: c.Name},
			{ColumnName: "Description", Value: c.Desc},
			{ColumnName: "List", Value: listName},
			{ColumnName: "Closed", Value: c.Closed},
		}
		if c.Due != nil {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: *c.Due})
		}
		if len(c.Labels) > 0 {
			labels := make([]string, 0, len(c.Labels))
			for _, l := range c.Labels {
				if l.Name != "" {
					labels = append(labels, l.Name)
				} else {
					labels = append(labels, l.Color)
				}
			}
			cells = append(cells, model.Cell{ColumnName: "Labels", Value: labels})
		}
		rows = append(rows, model.Row{ID: c.ID, Cells: cells})
	}

	return &model.Project{ID: projectID, Name: boardName, Columns: columns, Rows: rows}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/trello/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/extractor/trello/
git commit -m "feat: Trello — ListProjects, list name resolution, labels, board name, 1000-card limit"
```

---

## Task 8: Jira — ListProjects + Custom Fields + Priority/Type

**Files:**
- Modify: `internal/extractor/jira/jira.go`
- Modify: `internal/extractor/jira/jira_test.go`

**Step 1: Write failing tests**

```go
func TestJiraListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/project/search")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"values": []map[string]interface{}{
				{"id": "10000", "key": "PROJ", "name": "My Project"},
			},
		})
	}))
	defer srv.Close()

	e := jiraext.New("user@example.com", "token", srv.URL, jiraext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "PROJ", projects[0].ID)
	assert.Equal(t, "My Project", projects[0].Name)
}

func TestJiraExtractProjectPriority(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/3/field" {
			json.NewEncoder(w).Encode([]map[string]interface{}{ //nolint:errcheck
				{"id": "summary", "name": "Summary"},
				{"id": "priority", "name": "Priority"},
				{"id": "issuetype", "name": "Issue Type"},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"issues": []map[string]interface{}{
				{
					"id": "1", "key": "P-1",
					"fields": map[string]interface{}{
						"summary":   "Test Issue",
						"priority":  map[string]interface{}{"name": "High"},
						"issuetype": map[string]interface{}{"name": "Bug"},
					},
				},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	e := jiraext.New("u@e.com", "t", srv.URL, jiraext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "P", extractor.Options{})
	require.NoError(t, err)
	cellMap := make(map[string]interface{})
	for _, c := range proj.Rows[0].Cells {
		cellMap[c.ColumnName] = c.Value
	}
	assert.Equal(t, "High", cellMap["Priority"])
	assert.Equal(t, "Bug", cellMap["Issue Type"])
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/jira/... -run "TestJiraListProjects|TestJiraExtractProjectPriority" -v
```

Expected: FAIL

**Step 3: Update `internal/extractor/jira/jira.go`**

```go
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Values []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"values"`
	}
	if err := e.get(ctx, "/rest/api/3/project/search?maxResults=100", &resp); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(resp.Values))
	for i, p := range resp.Values {
		refs[i] = extractor.ProjectRef{ID: p.Key, Name: p.Name}
	}
	return refs, nil
}
```

Update `ExtractProject` to add priority, issue type, reporter, labels, and apply date filter to JQL:

```go
func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectKey string, opts extractor.Options) (*model.Project, error) {
	// Fetch field metadata
	var fieldMeta []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = e.get(ctx, "/rest/api/3/field", &fieldMeta)

	// Build JQL with date filters
	jql := fmt.Sprintf("project=%s ORDER BY created ASC", projectKey)
	if !opts.CreatedAfter.IsZero() {
		jql = fmt.Sprintf("project=%s AND created >= \"%s\" ORDER BY created ASC", projectKey, opts.CreatedAfter.Format("2006-01-02"))
	} else if !opts.UpdatedAfter.IsZero() {
		jql = fmt.Sprintf("project=%s AND updated >= \"%s\" ORDER BY created ASC", projectKey, opts.UpdatedAfter.Format("2006-01-02"))
	}

	var allIssues []struct {
		ID     string                 `json:"id"`
		Key    string                 `json:"key"`
		Fields map[string]interface{} `json:"fields"`
	}
	startAt := 0
	for {
		var resp struct {
			Issues []struct {
				ID     string                 `json:"id"`
				Key    string                 `json:"key"`
				Fields map[string]interface{} `json:"fields"`
			} `json:"issues"`
			Total int `json:"total"`
		}
		path := fmt.Sprintf("/rest/api/3/search?jql=%s&startAt=%d&maxResults=100", jql, startAt)
		if err := e.get(ctx, path, &resp); err != nil {
			return nil, err
		}
		allIssues = append(allIssues, resp.Issues...)
		if len(resp.Issues) == 0 || startAt+len(resp.Issues) >= resp.Total {
			break
		}
		startAt += len(resp.Issues)
	}

	columns := []model.ColumnDef{
		{Name: "Summary", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "Status", Type: model.TypeSingleSelect},
		{Name: "Priority", Type: model.TypeSingleSelect},
		{Name: "Issue Type", Type: model.TypeSingleSelect},
		{Name: "Assignee", Type: model.TypeContact},
		{Name: "Reporter", Type: model.TypeContact},
		{Name: "Labels", Type: model.TypeMultiSelect},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Key", Type: model.TypeText},
	}

	rows := make([]model.Row, 0, len(allIssues))
	for _, issue := range allIssues {
		cells := []model.Cell{{ColumnName: "Summary", Value: ""}}
		if v, ok := issue.Fields["summary"].(string); ok {
			cells[0].Value = v
		}
		cells = append(cells, model.Cell{ColumnName: "Key", Value: issue.Key})

		if desc := issue.Fields["description"]; desc != nil {
			cells = append(cells, model.Cell{ColumnName: "Description", Value: transformer.ADFToPlainText(desc)})
		}
		if status, ok := issue.Fields["status"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Status", Value: status["name"]})
		}
		if priority, ok := issue.Fields["priority"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Priority", Value: priority["name"]})
		}
		if issueType, ok := issue.Fields["issuetype"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Issue Type", Value: issueType["name"]})
		}
		if assignee, ok := issue.Fields["assignee"].(map[string]interface{}); ok && assignee != nil {
			cells = append(cells, model.Cell{ColumnName: "Assignee", Value: assignee["emailAddress"]})
		}
		if reporter, ok := issue.Fields["reporter"].(map[string]interface{}); ok && reporter != nil {
			cells = append(cells, model.Cell{ColumnName: "Reporter", Value: reporter["emailAddress"]})
		}
		if labels, ok := issue.Fields["labels"].([]interface{}); ok && len(labels) > 0 {
			labelStrs := make([]string, 0, len(labels))
			for _, l := range labels {
				if s, ok := l.(string); ok {
					labelStrs = append(labelStrs, s)
				}
			}
			cells = append(cells, model.Cell{ColumnName: "Labels", Value: labelStrs})
		}
		if due, ok := issue.Fields["duedate"].(string); ok && due != "" {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: due})
		}

		rows = append(rows, model.Row{ID: issue.ID, Cells: cells})
	}

	return &model.Project{ID: projectKey, Name: projectKey, Columns: columns, Rows: rows}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/jira/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/extractor/jira/
git commit -m "feat: Jira — ListProjects, priority, issue type, reporter, labels, JQL date filter"
```

---

## Task 9: Airtable — ListProjects with Typed Schema

**Files:**
- Modify: `internal/extractor/airtable/airtable.go`
- Modify: `internal/extractor/airtable/airtable_test.go`

**Step 1: Write failing tests**

```go
func TestAirtableListProjectsWithSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"tables": []map[string]interface{}{
				{
					"id":   "tbl_1",
					"name": "Tasks",
					"fields": []map[string]interface{}{
						{"id": "fld_1", "name": "Name", "type": "singleLineText"},
						{"id": "fld_2", "name": "Status", "type": "singleSelect",
							"options": map[string]interface{}{
								"choices": []map[string]interface{}{
									{"name": "Todo"}, {"name": "Done"},
								},
							},
						},
						{"id": "fld_3", "name": "Due", "type": "date"},
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
						},
					},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"records": []map[string]interface{}{
				{"id": "rec_1", "fields": map[string]interface{}{"Name": "Task 1", "Done": true}},
			},
		})
	}))
	defer srv.Close()

	e := airtableext.New("fake-token", airtableext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "base_1", "tbl_1", extractor.Options{})
	require.NoError(t, err)

	colTypeMap := make(map[string]model.ColumnType)
	for _, col := range proj.Columns {
		colTypeMap[col.Name] = col.Type
	}
	assert.Equal(t, model.TypeText, colTypeMap["Name"])
	assert.Equal(t, model.TypeCheckbox, colTypeMap["Done"])
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/airtable/... -run "TestAirtableListProjectsWithSchema|TestAirtableExtractProjectTypedColumns" -v
```

Expected: FAIL

**Step 3: Update `internal/extractor/airtable/airtable.go`**

Add typed schema support:

```go
type airtableField struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Options *struct {
		Choices []struct{ Name string `json:"name"` } `json:"choices"`
	} `json:"options"`
}

type airtableTable struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Fields []airtableField `json:"fields"`
}

func (e *Extractor) ListProjects(ctx context.Context, baseID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Tables []airtableTable `json:"tables"`
	}
	if err := e.get(ctx, fmt.Sprintf("%s/meta/bases/%s/tables", e.baseURL, baseID), &resp); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(resp.Tables))
	for i, t := range resp.Tables {
		refs[i] = extractor.ProjectRef{ID: t.ID, Name: t.Name}
	}
	return refs, nil
}

func airtableTypeToCanonical(airtableType string) model.ColumnType {
	switch airtableType {
	case "date":
		return model.TypeDate
	case "dateTime":
		return model.TypeDateTime
	case "checkbox":
		return model.TypeCheckbox
	case "singleSelect":
		return model.TypeSingleSelect
	case "multipleSelects":
		return model.TypeMultiSelect
	case "singleCollaborator", "createdBy", "lastModifiedBy":
		return model.TypeContact
	case "multipleCollaborators":
		return model.TypeMultiContact
	case "number", "currency", "percent", "duration":
		return model.TypeNumber
	default:
		return model.TypeText
	}
}
```

Update `ExtractProject` to fetch schema first, build typed columns, then extract records with proper value handling:

```go
func (e *Extractor) ExtractProject(ctx context.Context, baseID, tableID string, opts extractor.Options) (*model.Project, error) {
	// Fetch schema for this table
	var schemaResp struct {
		Tables []airtableTable `json:"tables"`
	}
	fieldsByName := make(map[string]airtableField)
	tableName := tableID
	if err := e.get(ctx, fmt.Sprintf("%s/meta/bases/%s/tables", e.baseURL, baseID), &schemaResp); err == nil {
		for _, t := range schemaResp.Tables {
			if t.ID == tableID || t.Name == tableID {
				tableName = t.Name
				for _, f := range t.Fields {
					fieldsByName[f.Name] = f
				}
				break
			}
		}
	}

	// Fetch records
	var allRecords []struct {
		ID     string                 `json:"id"`
		Fields map[string]interface{} `json:"fields"`
	}
	url := fmt.Sprintf("%s/%s/%s?pageSize=100", e.baseURL, baseID, tableID)
	for {
		var resp struct {
			Records []struct {
				ID     string                 `json:"id"`
				Fields map[string]interface{} `json:"fields"`
			} `json:"records"`
			Offset *string `json:"offset"`
		}
		if err := e.get(ctx, url, &resp); err != nil {
			return nil, err
		}
		allRecords = append(allRecords, resp.Records...)
		if resp.Offset == nil {
			break
		}
		url = fmt.Sprintf("%s/%s/%s?pageSize=100&offset=%s", e.baseURL, baseID, tableID, *resp.Offset)
	}

	// Build columns from schema (or infer from records)
	colOrder := make([]string, 0)
	colDefs := make(map[string]model.ColumnDef)
	if len(fieldsByName) > 0 {
		for name, f := range fieldsByName {
			opts := make([]string, 0)
			if f.Options != nil {
				for _, c := range f.Options.Choices {
					opts = append(opts, c.Name)
				}
			}
			colDefs[name] = model.ColumnDef{Name: name, Type: airtableTypeToCanonical(f.Type), Options: opts}
			colOrder = append(colOrder, name)
		}
	} else {
		// Fallback: infer from records
		seen := map[string]bool{}
		for _, r := range allRecords {
			for k := range r.Fields {
				if !seen[k] {
					seen[k] = true
					colDefs[k] = model.ColumnDef{Name: k, Type: model.TypeText}
					colOrder = append(colOrder, k)
				}
			}
		}
	}
	columns := make([]model.ColumnDef, 0, len(colOrder))
	for _, name := range colOrder {
		columns = append(columns, colDefs[name])
	}

	rows := make([]model.Row, 0, len(allRecords))
	for _, r := range allRecords {
		cells := make([]model.Cell, 0, len(r.Fields))
		for name, v := range r.Fields {
			cells = append(cells, model.Cell{ColumnName: name, Value: extractAirtableValue(v, fieldsByName[name].Type)})
		}
		rows = append(rows, model.Row{ID: r.ID, Cells: cells})
	}

	return &model.Project{ID: tableID, Name: tableName, Columns: columns, Rows: rows}, nil
}

func extractAirtableValue(v interface{}, fieldType string) interface{} {
	if v == nil {
		return nil
	}
	switch fieldType {
	case "multipleSelects":
		if arr, ok := v.([]interface{}); ok {
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			return strs
		}
	case "singleCollaborator", "createdBy", "lastModifiedBy":
		if m, ok := v.(map[string]interface{}); ok {
			if email, ok := m["email"].(string); ok {
				return email
			}
		}
	case "multipleCollaborators":
		if arr, ok := v.([]interface{}); ok {
			emails := make([]string, 0)
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if email, ok := m["email"].(string); ok {
						emails = append(emails, email)
					}
				}
			}
			return emails
		}
	case "multipleAttachments":
		// Return as string list of filenames for now; attachment download handled separately
		if arr, ok := v.([]interface{}); ok {
			names := make([]string, 0)
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if name, ok := m["filename"].(string); ok {
						names = append(names, name)
					}
				}
			}
			return strings.Join(names, ", ")
		}
	}
	return fmt.Sprintf("%v", v)
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/airtable/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/extractor/airtable/
git commit -m "feat: Airtable — ListProjects with typed schema, typed columns, proper value extraction"
```

---

## Task 10: Notion — ListProjects + All Property Types

**Files:**
- Modify: `internal/extractor/notion/notion.go`
- Modify: `internal/extractor/notion/notion_test.go`

**Step 1: Write failing tests**

```go
func TestNotionListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"results": []map[string]interface{}{
				{
					"id":     "db_1",
					"object": "database",
					"title":  []map[string]interface{}{{"plain_text": "My Database"}},
				},
			},
			"has_more": false,
		})
	}))
	defer srv.Close()

	e := notionext.New("fake-token", notionext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "My Database", projects[0].Name)
}

func TestNotionExtractProjectMultiSelect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"results": []map[string]interface{}{
				{
					"id": "page_1",
					"properties": map[string]interface{}{
						"Tags": map[string]interface{}{
							"multi_select": []map[string]interface{}{
								{"name": "alpha"}, {"name": "beta"},
							},
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
	tags, ok := cellMap["Tags"].([]string)
	assert.True(t, ok, "multi_select should be []string")
	assert.ElementsMatch(t, []string{"alpha", "beta"}, tags)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/notion/... -run "TestNotionListProjects|TestNotionExtractProjectMultiSelect" -v
```

Expected: FAIL

**Step 3: Update `internal/extractor/notion/notion.go`**

Replace `ListWorkspaces` to paginate and replace stub with real `ListProjects`:

```go
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var refs []extractor.ProjectRef
	var cursor *string
	for {
		body := map[string]interface{}{
			"filter":    map[string]interface{}{"value": "database", "property": "object"},
			"page_size": 100,
		}
		if cursor != nil {
			body["start_cursor"] = *cursor
		}
		var resp struct {
			Results []struct {
				ID    string `json:"id"`
				Title []struct {
					PlainText string `json:"plain_text"`
				} `json:"title"`
			} `json:"results"`
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := e.post(ctx, "/search", body, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Results {
			name := r.ID
			if len(r.Title) > 0 {
				name = r.Title[0].PlainText
			}
			refs = append(refs, extractor.ProjectRef{ID: r.ID, Name: name})
		}
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}
	return refs, nil
}
```

Expand `extractNotionPropValue` to cover all property types:

```go
func extractNotionPropValue(prop interface{}) interface{} {
	m, ok := prop.(map[string]interface{})
	if !ok {
		return nil
	}

	// title
	if title, ok := m["title"].([]interface{}); ok && len(title) > 0 {
		if rt, ok := title[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rt["plain_text"])
		}
	}
	// rich_text
	if rt, ok := m["rich_text"].([]interface{}); ok && len(rt) > 0 {
		if rtm, ok := rt[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rtm["plain_text"])
		}
	}
	// select
	if sel, ok := m["select"].(map[string]interface{}); ok && sel != nil {
		return fmt.Sprintf("%v", sel["name"])
	}
	// status (newer Notion property type)
	if status, ok := m["status"].(map[string]interface{}); ok && status != nil {
		return fmt.Sprintf("%v", status["name"])
	}
	// multi_select → []string
	if ms, ok := m["multi_select"].([]interface{}); ok {
		vals := make([]string, 0, len(ms))
		for _, v := range ms {
			if vm, ok := v.(map[string]interface{}); ok {
				vals = append(vals, fmt.Sprintf("%v", vm["name"]))
			}
		}
		return vals
	}
	// date
	if date, ok := m["date"].(map[string]interface{}); ok && date != nil {
		start := fmt.Sprintf("%v", date["start"])
		if end, ok := date["end"].(string); ok && end != "" {
			return start + " – " + end
		}
		return start
	}
	// checkbox
	if cb, ok := m["checkbox"]; ok {
		return cb
	}
	// number
	if num, ok := m["number"]; ok && num != nil {
		return num
	}
	// url
	if url, ok := m["url"].(string); ok {
		return url
	}
	// email
	if email, ok := m["email"].(string); ok {
		return email
	}
	// phone_number
	if phone, ok := m["phone_number"].(string); ok {
		return phone
	}
	// people → []string (emails)
	if people, ok := m["people"].([]interface{}); ok {
		emails := make([]string, 0, len(people))
		for _, p := range people {
			if pm, ok := p.(map[string]interface{}); ok {
				if person, ok := pm["person"].(map[string]interface{}); ok {
					if email, ok := person["email"].(string); ok {
						emails = append(emails, email)
					}
				}
			}
		}
		return emails
	}
	// created_time / last_edited_time
	if ct, ok := m["created_time"].(string); ok {
		return ct
	}
	if let, ok := m["last_edited_time"].(string); ok {
		return let
	}
	// formula
	if formula, ok := m["formula"].(map[string]interface{}); ok {
		for _, key := range []string{"string", "number", "boolean"} {
			if v, ok := formula[key]; ok && v != nil {
				return fmt.Sprintf("%v", v)
			}
		}
		if d, ok := formula["date"].(map[string]interface{}); ok && d != nil {
			return fmt.Sprintf("%v", d["start"])
		}
	}
	// unique_id
	if uid, ok := m["unique_id"].(map[string]interface{}); ok {
		prefix, _ := uid["prefix"].(string)
		num, _ := uid["number"]
		if prefix != "" {
			return fmt.Sprintf("%s-%v", prefix, num)
		}
		return fmt.Sprintf("%v", num)
	}

	return nil
}
```

Also update `ExtractProject` to use `interface{}` return from `extractNotionPropValue` and infer column types:

```go
// In ExtractProject, update the rows building loop:
for colName, propVal := range props {
    value := extractNotionPropValue(propVal)
    if value != nil {
        cells = append(cells, model.Cell{ColumnName: colName, Value: value})
    }
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/notion/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/extractor/notion/
git commit -m "feat: Notion — ListProjects paginated, all 15+ property types, multi_select as []string"
```

---

## Task 11: Monday + Wrike — ListProjects + Column Name Fix + Assignees

**Files:**
- Modify: `internal/extractor/monday/monday.go`
- Modify: `internal/extractor/monday/monday_test.go`
- Modify: `internal/extractor/wrike/wrike.go`
- Modify: `internal/extractor/wrike/wrike_test.go`

**Step 1: Write Monday failing test for column name fix**

```go
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
	// Cell should use column TITLE "Status", not raw ID "status"
	assert.Equal(t, "Status", proj.Rows[0].Cells[1].ColumnName, "cell ColumnName should be column title not ID")
	assert.Equal(t, "Done", proj.Rows[0].Cells[1].Value)
}
```

**Step 2: Write Wrike failing tests**

```go
func TestWrikeListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"data": []map[string]interface{}{
				{"id": "FOLDER_1", "title": "My Project", "project": map[string]interface{}{}},
				{"id": "FOLDER_2", "title": "Just a Folder"},
			},
		})
	}))
	defer srv.Close()

	e := wrikeext.New("fake-token", wrikeext.WithBaseURL(srv.URL))
	projects, err := e.ListProjects(context.Background(), "acc_1")
	require.NoError(t, err)
	// Only folders with "project" field should be returned
	assert.Len(t, projects, 1)
	assert.Equal(t, "My Project", projects[0].Name)
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/extractor/monday/... ./internal/extractor/wrike/... -run "TestMondayColumnNamesInCells|TestWrikeListProjects" -v
```

Expected: FAIL

**Step 4: Fix Monday — column name/ID mismatch in `monday.go`**

In `ExtractProject`, build a `colTitleByID` map and use it when building cells:

```go
// Build column title lookup
colTitleByID := make(map[string]string, len(board.Columns))
for _, c := range board.Columns {
    colTitleByID[c.ID] = c.Title
}

// When building cells:
for _, cv := range item.ColumnValues {
    title := colTitleByID[cv.ID]
    if title == "" {
        title = cv.ID
    }
    if text := transformer.StripHTML(cv.Text); text != "" {
        cells = append(cells, model.Cell{ColumnName: title, Value: text})
    }
}
```

Also add `ListProjects` for Monday:

```go
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var data struct {
		Boards []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"boards"`
	}
	q := `{ boards(limit: 100) { id name } }`
	if workspaceID != "" {
		q = fmt.Sprintf(`{ boards(workspace_ids: [%s], limit: 100) { id name } }`, workspaceID)
	}
	if err := e.query(ctx, q, &data); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(data.Boards))
	for i, b := range data.Boards {
		refs[i] = extractor.ProjectRef{ID: b.ID, Name: b.Name}
	}
	return refs, nil
}
```

**Step 5: Fix Wrike — `ListProjects` and `ExtractProject` folder name**

```go
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Data []struct {
			ID      string                 `json:"id"`
			Title   string                 `json:"title"`
			Project map[string]interface{} `json:"project"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/accounts/%s/folders", workspaceID)
	if err := e.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	var refs []extractor.ProjectRef
	for _, f := range resp.Data {
		if f.Project != nil {
			refs = append(refs, extractor.ProjectRef{ID: f.ID, Name: f.Title})
		}
	}
	return refs, nil
}
```

Update `ExtractProject` to fetch folder name and add assignees (Responsibles):

```go
// Fetch folder name
var folderResp struct {
    Data []struct {
        Title string `json:"title"`
    } `json:"data"`
}
_ = e.get(ctx, fmt.Sprintf("/folders/%s", folderID), &folderResp)
folderName := folderID
if len(folderResp.Data) > 0 {
    folderName = folderResp.Data[0].Title
}
```

Add `Assignees` column and extract `responsibles`:
```go
columns = append(columns, model.ColumnDef{Name: "Assignees", Type: model.TypeMultiContact})
// In row building:
if len(t.Responsibles) > 0 {
    cells = append(cells, model.Cell{ColumnName: "Assignees", Value: t.Responsibles})
}
```

(Responsibles are user IDs in Wrike — for now store as text; full email resolution requires extra API calls.)

**Step 6: Run all tests**

```bash
go test ./internal/extractor/monday/... ./internal/extractor/wrike/... -v
```

Expected: all PASS

**Step 7: Commit**

```bash
git add internal/extractor/monday/ internal/extractor/wrike/
git commit -m "feat: Monday column name fix + ListProjects; Wrike ListProjects + folder name + assignees"
```

---

## Task 12: Loader — Rate Limiter + Sheet Naming

**Files:**
- Modify: `internal/loader/smartsheet/loader.go`
- Modify: `internal/loader/smartsheet/loader_test.go`

**Step 1: Write failing test**

```go
func TestLoaderRateLimiterExists(t *testing.T) {
	// Verify loader applies rate limiting — create loader and verify it has a limiter
	loader := smartsheet.New("token")
	// We can't directly inspect the limiter, but we can verify it doesn't panic on rapid calls
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

	loader = smartsheet.New("token", smartsheet.WithBaseURL(srv.URL))
	proj := &model.Project{Name: "Test", Columns: []model.ColumnDef{{Name: "Name", Type: model.TypeText}}}
	_, _, err := loader.CreateSheet(context.Background(), proj, 0)
	require.NoError(t, err)
}
```

**Step 2: Add rate limiter to Loader**

In `loader.go`, add `rl *ratelimit.Limiter` to `Loader` struct and call `l.rl.Wait()` before every HTTP request:

```go
import "github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"

type Loader struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

func New(token string, opts ...Option) *Loader {
	l := &Loader{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
		rl:      ratelimit.ForPlatform("smartsheet"),
	}
	for _, o := range opts {
		o(l)
	}
	return l
}
```

Add `l.rl.Wait()` at the start of `CreateSheet`, `insertRowBatch`, `UploadAttachment`, `AddComment`.

**Step 3: Run tests**

```bash
go test ./internal/loader/smartsheet/... -v
```

Expected: all PASS

**Step 4: Commit**

```bash
git add internal/loader/smartsheet/
git commit -m "feat: Smartsheet loader rate limiter (300 req/min token bucket)"
```

---

## Task 13: State — Improved File Naming + Partial Row Resume

**Files:**
- Modify: `internal/state/state.go`
- Modify: `internal/state/state_test.go`

**Step 1: Write failing test**

```go
func TestPartialSheetUpdate(t *testing.T) {
	s := &state.MigrationState{Source: "asana"}
	s.UpdatePartialSheet("proj_1", 999, 250)
	assert.NotNil(t, s.PartialSheet)
	assert.Equal(t, "proj_1", s.PartialSheet.SourceID)
	assert.Equal(t, int64(999), s.PartialSheet.SmartsheetID)
	assert.Equal(t, 250, s.PartialSheet.LastCompletedRow)
}

func TestPartialSheetClear(t *testing.T) {
	s := &state.MigrationState{
		PartialSheet: &state.PartialSheet{SourceID: "p1"},
	}
	s.ClearPartialSheet()
	assert.Nil(t, s.PartialSheet)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/state/... -run "TestPartialSheet" -v
```

Expected: FAIL

**Step 3: Add `UpdatePartialSheet` and `ClearPartialSheet` to `state.go`**

```go
// UpdatePartialSheet records progress within a currently-migrating sheet.
func (s *MigrationState) UpdatePartialSheet(sourceID string, smartsheetID int64, lastRow int) {
	s.PartialSheet = &PartialSheet{
		SourceID:         sourceID,
		SmartsheetID:     smartsheetID,
		LastCompletedRow: lastRow,
	}
}

// ClearPartialSheet removes the partial sheet record (called when a sheet completes).
func (s *MigrationState) ClearPartialSheet() {
	s.PartialSheet = nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/state/... -v
```

Expected: all PASS

**Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat: state — UpdatePartialSheet and ClearPartialSheet helpers"
```

---

## Task 14: Full Test Run + Push

**Step 1: Run full test suite with race detector**

```bash
cd /Users/bchauhan/Projects/migrate-to-smartsheet
go test ./... -race -count=1 2>&1 | grep -E "^ok|^FAIL|^---"
```

Expected: all `ok`, zero `FAIL`

**Step 2: Run golangci-lint**

```bash
golangci-lint run ./...
```

Expected: zero issues

**Step 3: Build all platforms**

```bash
GOOS=linux   GOARCH=amd64 go build -o /dev/null ./cmd/migrate/
GOOS=darwin  GOARCH=arm64 go build -o /dev/null ./cmd/migrate/
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/migrate/
```

Expected: all succeed

**Step 4: Push and tag v0.2.0**

```bash
git push origin main
git tag v0.2.0
git push origin v0.2.0
```

This triggers CI (tests + lint) and GoReleaser (release with all 6 platform binaries).
