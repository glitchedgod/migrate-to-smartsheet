# Smartsheet Migration Tool — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go CLI tool that migrates data from 7 competitor platforms (Asana, Monday.com, Trello, Jira, Airtable, Notion, Wrike) into Smartsheet as a single distributable binary.

**Architecture:** Monolithic CLI with embedded adapters. Each source platform has its own Extractor implementing a common interface. Data flows through a canonical model, transformer pipeline, and Smartsheet loader. State file enables resumability.

**Tech Stack:** Go 1.22+, cobra (CLI), survey/v2 (interactive prompts), progressbar/v3 (progress), go-graphql-client (Monday.com), golang.org/x/net/html (HTML stripping), golang.org/x/time/rate (rate limiting), testify (tests), GoReleaser (distribution)

---

## Task 1: Repository Scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/migrate/main.go`
- Create: `.gitignore`
- Create: `.goreleaser.yaml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

**Step 1: Initialize Go module**

```bash
cd /Users/bchauhan/Projects/migrate-to-smartsheet
go mod init github.com/bchauhan/migrate-to-smartsheet
```

Expected: `go.mod` created with `module github.com/bchauhan/migrate-to-smartsheet` and `go 1.22`

**Step 2: Install dependencies**

```bash
go get github.com/spf13/cobra@latest
go get github.com/AlecAivazis/survey/v2@latest
go get github.com/schollz/progressbar/v3@latest
go get github.com/hasura/go-graphql-client@latest
go get golang.org/x/net/html@latest
go get golang.org/x/time/rate@latest
go get github.com/stretchr/testify@latest
```

**Step 3: Create `cmd/migrate/main.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "migrate-to-smartsheet",
		Short: "Migrate data from project management tools into Smartsheet",
		RunE:  runMigrate,
	}

	root.PersistentFlags().String("source", "", "Source platform (asana|monday|trello|jira|airtable|notion|wrike)")
	root.PersistentFlags().String("source-token", "", "Source platform API token")
	root.PersistentFlags().String("source-key", "", "Trello API key (Trello only)")
	root.PersistentFlags().String("smartsheet-token", "", "Smartsheet API token")
	root.PersistentFlags().String("workspace", "", "Source workspace name")
	root.PersistentFlags().String("projects", "", "Comma-separated project names (omit = all)")
	root.PersistentFlags().String("created-after", "", "Only migrate items created after this date (ISO 8601)")
	root.PersistentFlags().String("updated-after", "", "Only migrate items updated after this date (ISO 8601)")
	root.PersistentFlags().String("user-map", "", "Path to CSV file mapping source user IDs to Smartsheet emails")
	root.PersistentFlags().String("sheet-prefix", "", "Prefix to add to each created sheet name")
	root.PersistentFlags().String("sheet-suffix", "", "Suffix to add to each created sheet name")
	root.PersistentFlags().String("sheet-name-template", "{project}", "Sheet naming template: {source}, {project}, {date}")
	root.PersistentFlags().String("conflict", "skip", "Conflict handling: skip|rename|overwrite")
	root.PersistentFlags().String("exclude-fields", "", "Comma-separated field names to exclude")
	root.PersistentFlags().String("jira-email", "", "Jira account email (Jira only)")
	root.PersistentFlags().String("jira-instance", "", "Jira Cloud URL (e.g. https://yourorg.atlassian.net)")
	root.PersistentFlags().Bool("include-attachments", true, "Download and re-upload attachments")
	root.PersistentFlags().Bool("include-comments", true, "Migrate comments")
	root.PersistentFlags().Bool("include-archived", false, "Include archived/completed items")
	root.PersistentFlags().Bool("yes", false, "Skip confirmation prompt")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrate(cmd *cobra.Command, args []string) error {
	// Implemented in run.go
	return nil
}
```

**Step 4: Create `.gitignore`**

```
migrate-to-smartsheet
dist/
.migrate-state-*.json
coverage.out
.idea/
.vscode/
*.swp
```

**Step 5: Build to verify it compiles**

```bash
go build ./cmd/migrate/
```

Expected: binary `migrate-to-smartsheet` created, no errors

**Step 6: Commit**

```bash
git init
git add go.mod go.sum cmd/ .gitignore
git commit -m "feat: scaffold Go module and cobra CLI entry point"
```

---

## Task 2: Canonical Data Model

**Files:**
- Create: `pkg/model/model.go`
- Create: `pkg/model/model_test.go`

**Step 1: Write the failing test**

```go
// pkg/model/model_test.go
package model_test

import (
	"testing"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestWorkspaceHierarchy(t *testing.T) {
	ws := model.Workspace{
		ID:   "ws1",
		Name: "My Workspace",
		Projects: []model.Project{
			{
				ID:   "proj1",
				Name: "Alpha",
				Columns: []model.ColumnDef{
					{Name: "Title", Type: model.TypeText},
					{Name: "Due Date", Type: model.TypeDate},
					{Name: "Status", Type: model.TypeSingleSelect, Options: []string{"Todo", "Done"}},
				},
				Rows: []model.Row{
					{
						ID: "row1",
						Cells: []model.Cell{
							{ColumnName: "Title", Value: "First task"},
							{ColumnName: "Due Date", Value: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
							{ColumnName: "Status", Value: "Todo"},
						},
					},
				},
			},
		},
	}

	assert.Equal(t, "ws1", ws.ID)
	assert.Len(t, ws.Projects, 1)
	assert.Len(t, ws.Projects[0].Rows, 1)
	assert.Equal(t, model.TypeDate, ws.Projects[0].Columns[1].Type)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./pkg/model/... -v
```

Expected: FAIL — `model` package does not exist yet

**Step 3: Create `pkg/model/model.go`**

```go
package model

import "time"

type ColumnType string

const (
	TypeText         ColumnType = "Text"
	TypeNumber       ColumnType = "Number"
	TypeDate         ColumnType = "Date"
	TypeDateTime     ColumnType = "DateTime"
	TypeCheckbox     ColumnType = "Checkbox"
	TypeSingleSelect ColumnType = "SingleSelect"
	TypeMultiSelect  ColumnType = "MultiSelect"
	TypeContact      ColumnType = "Contact"
	TypeMultiContact ColumnType = "MultiContact"
	TypeURL          ColumnType = "URL"
	TypeDuration     ColumnType = "Duration"
)

type ColumnDef struct {
	Name    string
	Type    ColumnType
	Options []string
}

type Cell struct {
	ColumnName string
	Value      interface{}
}

type Attachment struct {
	Name        string
	URL         string
	ContentType string
	SizeBytes   int64
	ExpiresAt   time.Time
}

type Comment struct {
	AuthorEmail string
	AuthorName  string
	Body        string
	CreatedAt   time.Time
}

type Row struct {
	ID          string
	ParentID    string
	Cells       []Cell
	Attachments []Attachment
	Comments    []Comment
}

type Project struct {
	ID      string
	Name    string
	Columns []ColumnDef
	Rows    []Row
}

type Workspace struct {
	ID       string
	Name     string
	Projects []Project
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./pkg/model/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add pkg/
git commit -m "feat: canonical data model (Workspace/Project/Row/Cell/Attachment/Comment)"
```

---

## Task 3: Transformer — Field Type Mapping

**Files:**
- Create: `internal/transformer/fieldmap.go`
- Create: `internal/transformer/fieldmap_test.go`

**Step 1: Write failing tests**

```go
// internal/transformer/fieldmap_test.go
package transformer_test

import (
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestToSmartsheetColumnType(t *testing.T) {
	cases := []struct {
		in  model.ColumnType
		out string
	}{
		{model.TypeText, "TEXT_NUMBER"},
		{model.TypeNumber, "TEXT_NUMBER"},
		{model.TypeDate, "DATE"},
		{model.TypeDateTime, "DATETIME"},
		{model.TypeCheckbox, "CHECKBOX"},
		{model.TypeSingleSelect, "PICKLIST"},
		{model.TypeMultiSelect, "MULTI_PICKLIST"},
		{model.TypeContact, "CONTACT_LIST"},
		{model.TypeMultiContact, "MULTI_CONTACT_LIST"},
		{model.TypeURL, "TEXT_NUMBER"},
		{model.TypeDuration, "TEXT_NUMBER"},
	}
	for _, c := range cases {
		assert.Equal(t, c.out, transformer.ToSmartsheetColumnType(c.in), "input: %s", c.in)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/transformer/... -v
```

Expected: FAIL

**Step 3: Create `internal/transformer/fieldmap.go`**

```go
package transformer

import "github.com/bchauhan/migrate-to-smartsheet/pkg/model"

func ToSmartsheetColumnType(t model.ColumnType) string {
	switch t {
	case model.TypeDate:
		return "DATE"
	case model.TypeDateTime:
		return "DATETIME"
	case model.TypeCheckbox:
		return "CHECKBOX"
	case model.TypeSingleSelect:
		return "PICKLIST"
	case model.TypeMultiSelect:
		return "MULTI_PICKLIST"
	case model.TypeContact:
		return "CONTACT_LIST"
	case model.TypeMultiContact:
		return "MULTI_CONTACT_LIST"
	default:
		return "TEXT_NUMBER"
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/transformer/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/transformer/fieldmap.go internal/transformer/fieldmap_test.go
git commit -m "feat: transformer fieldmap — canonical type to Smartsheet column type"
```

---

## Task 4: Transformer — Rich Text Stripping

**Files:**
- Create: `internal/transformer/richtext.go`
- Create: `internal/transformer/richtext_test.go`

**Step 1: Write failing tests**

```go
// internal/transformer/richtext_test.go
package transformer_test

import (
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/stretchr/testify/assert"
)

func TestStripHTML(t *testing.T) {
	assert.Equal(t, "Hello world", transformer.StripHTML("<p>Hello <b>world</b></p>"))
	assert.Equal(t, "Line 1 Line 2", transformer.StripHTML("<p>Line 1</p><p>Line 2</p>"))
	assert.Equal(t, "plain text", transformer.StripHTML("plain text"))
	assert.Equal(t, "", transformer.StripHTML(""))
}

func TestADFToPlainText(t *testing.T) {
	adf := map[string]interface{}{
		"type": "doc",
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Hello from Jira",
					},
				},
			},
		},
	}
	assert.Equal(t, "Hello from Jira", transformer.ADFToPlainText(adf))
}

func TestADFToPlainTextNil(t *testing.T) {
	assert.Equal(t, "", transformer.ADFToPlainText(nil))
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/transformer/... -run TestStripHTML -v
```

Expected: FAIL

**Step 3: Create `internal/transformer/richtext.go`**

```go
package transformer

import (
	"strings"

	"golang.org/x/net/html"
)

func StripHTML(input string) string {
	if input == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return input
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return b.String()
}

func ADFToPlainText(node interface{}) string {
	if node == nil {
		return ""
	}
	m, ok := node.(map[string]interface{})
	if !ok {
		return ""
	}
	var b strings.Builder
	walkADF(m, &b)
	return strings.TrimSpace(b.String())
}

func walkADF(node map[string]interface{}, b *strings.Builder) {
	if t, ok := node["type"].(string); ok && t == "text" {
		if text, ok := node["text"].(string); ok {
			b.WriteString(text)
		}
		return
	}
	content, ok := node["content"].([]interface{})
	if !ok {
		return
	}
	for _, child := range content {
		if cm, ok := child.(map[string]interface{}); ok {
			walkADF(cm, b)
		}
	}
	if t, ok := node["type"].(string); ok {
		switch t {
		case "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock":
			b.WriteByte('\n')
		}
	}
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/transformer/... -v
```

Expected: PASS all

**Step 5: Commit**

```bash
git add internal/transformer/richtext.go internal/transformer/richtext_test.go
git commit -m "feat: transformer richtext — HTML stripping and Jira ADF to plain text"
```

---

## Task 5: Transformer — User Remapping

**Files:**
- Create: `internal/transformer/usermap.go`
- Create: `internal/transformer/usermap_test.go`

**Step 1: Write failing test**

```go
// internal/transformer/usermap_test.go
package transformer_test

import (
	"strings"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadUserMap(t *testing.T) {
	csv := "source_id,smartsheet_email\nuser123,alice@example.com\nuser456,bob@example.com\n"
	um, err := transformer.LoadUserMapFromReader(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", um.Lookup("user123"))
	assert.Equal(t, "bob@example.com", um.Lookup("user456"))
	assert.Equal(t, "", um.Lookup("unknown"))
}

func TestLoadUserMapEmpty(t *testing.T) {
	um, err := transformer.LoadUserMapFromReader(strings.NewReader("source_id,smartsheet_email\n"))
	require.NoError(t, err)
	assert.Equal(t, "", um.Lookup("anything"))
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/transformer/... -run TestLoadUserMap -v
```

Expected: FAIL

**Step 3: Create `internal/transformer/usermap.go`**

```go
package transformer

import (
	"encoding/csv"
	"io"
)

type UserMap struct {
	m map[string]string
}

func (u *UserMap) Lookup(sourceID string) string {
	if u.m == nil {
		return ""
	}
	return u.m[sourceID]
}

func LoadUserMapFromReader(r io.Reader) (*UserMap, error) {
	records, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for i, row := range records {
		if i == 0 {
			continue // skip header
		}
		if len(row) >= 2 {
			m[row[0]] = row[1]
		}
	}
	return &UserMap{m: m}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/transformer/... -v
```

Expected: PASS all

**Step 5: Commit**

```bash
git add internal/transformer/usermap.go internal/transformer/usermap_test.go
git commit -m "feat: transformer usermap — load CSV source-to-Smartsheet email mapping"
```

---

## Task 6: Rate Limiter

**Files:**
- Create: `internal/ratelimit/ratelimit.go`
- Create: `internal/ratelimit/ratelimit_test.go`

**Step 1: Write failing test**

```go
// internal/ratelimit/ratelimit_test.go
package ratelimit_test

import (
	"testing"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/stretchr/testify/assert"
)

func TestRateLimiterForPlatform(t *testing.T) {
	rl := ratelimit.ForPlatform("notion")
	assert.NotNil(t, rl)
	start := time.Now()
	rl.Wait()
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 500*time.Millisecond, "first token should be immediate")
}

func TestRateLimiterUnknownPlatform(t *testing.T) {
	rl := ratelimit.ForPlatform("unknown")
	assert.NotNil(t, rl)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/ratelimit/... -v
```

Expected: FAIL

**Step 3: Create `internal/ratelimit/ratelimit.go`**

```go
package ratelimit

import (
	"context"

	"golang.org/x/time/rate"
)

type Limiter struct {
	l *rate.Limiter
}

func (l *Limiter) Wait() {
	l.l.Wait(context.Background()) //nolint:errcheck
}

func ForPlatform(platform string) *Limiter {
	var r rate.Limit
	var burst int
	switch platform {
	case "notion":
		r, burst = 3, 3
	case "airtable":
		r, burst = 5, 5
	case "wrike":
		r, burst = 2, 5
	case "trello":
		r, burst = 30, 30
	case "asana":
		r, burst = 25, 50
	case "jira":
		r, burst = 50, 50
	case "monday":
		r, burst = 10, 10
	case "smartsheet":
		r, burst = 5, 10
	default:
		r, burst = 5, 5
	}
	return &Limiter{l: rate.NewLimiter(r, burst)}
}
```

**Step 4: Run tests**

```bash
go test ./internal/ratelimit/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/ratelimit/
git commit -m "feat: per-platform rate limiter using token bucket"
```

---

## Task 7: State / Resumability

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/state/state_test.go`

**Step 1: Write failing tests**

```go
// internal/state/state_test.go
package state_test

import (
	"os"
	"testing"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := &state.MigrationState{
		Source:          "asana",
		StartedAt:       time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC),
		CompletedSheets: []string{"sheet_1", "sheet_2"},
	}
	path := dir + "/.migrate-state-2026-03-01.json"
	require.NoError(t, state.Save(path, s))

	loaded, err := state.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "asana", loaded.Source)
	assert.Equal(t, []string{"sheet_1", "sheet_2"}, loaded.CompletedSheets)
}

func TestLoadMissing(t *testing.T) {
	_, err := state.Load("/nonexistent/path.json")
	assert.True(t, os.IsNotExist(err))
}

func TestIsCompleted(t *testing.T) {
	s := &state.MigrationState{CompletedSheets: []string{"a", "b"}}
	assert.True(t, s.IsCompleted("a"))
	assert.False(t, s.IsCompleted("c"))
}

func TestMarkCompleted(t *testing.T) {
	s := &state.MigrationState{}
	s.MarkCompleted("proj1")
	s.MarkCompleted("proj1")
	assert.Len(t, s.CompletedSheets, 1)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/state/... -v
```

Expected: FAIL

**Step 3: Create `internal/state/state.go`**

```go
package state

import (
	"encoding/json"
	"os"
	"time"
)

type PartialSheet struct {
	SourceID         string `json:"source_id"`
	SmartsheetID     int64  `json:"smartsheet_id"`
	LastCompletedRow int    `json:"last_completed_row"`
}

type MigrationState struct {
	Source          string        `json:"source"`
	StartedAt       time.Time     `json:"started_at"`
	CompletedSheets []string      `json:"completed_sheets"`
	PartialSheet    *PartialSheet `json:"partial_sheet,omitempty"`
}

func (s *MigrationState) IsCompleted(sourceProjectID string) bool {
	for _, id := range s.CompletedSheets {
		if id == sourceProjectID {
			return true
		}
	}
	return false
}

func (s *MigrationState) MarkCompleted(sourceProjectID string) {
	if s.IsCompleted(sourceProjectID) {
		return
	}
	s.CompletedSheets = append(s.CompletedSheets, sourceProjectID)
}

func Save(path string, s *MigrationState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func Load(path string) (*MigrationState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s MigrationState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/state/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat: migration state — save/load/resume JSON state file"
```

---

## Task 8: Extractor Interface

**Files:**
- Create: `internal/extractor/extractor.go`
- Create: `internal/extractor/extractor_test.go`

**Step 1: Write failing test**

```go
// internal/extractor/extractor_test.go
package extractor_test

import (
	"context"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

type mockExtractor struct{}

func (m *mockExtractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	return []model.Workspace{{ID: "ws1", Name: "Test WS"}}, nil
}
func (m *mockExtractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	return &model.Project{ID: projectID, Name: "Test Project"}, nil
}

func TestExtractorInterface(t *testing.T) {
	var e extractor.Extractor = &mockExtractor{}
	workspaces, err := e.ListWorkspaces(context.Background())
	assert.NoError(t, err)
	assert.Len(t, workspaces, 1)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/extractor/... -v
```

Expected: FAIL

**Step 3: Create `internal/extractor/extractor.go`**

```go
package extractor

import (
	"context"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

type Options struct {
	IncludeAttachments bool
	IncludeComments    bool
	IncludeArchived    bool
	CreatedAfter       time.Time
	UpdatedAfter       time.Time
	ExcludeFields      []string
}

type Extractor interface {
	ListWorkspaces(ctx context.Context) ([]model.Workspace, error)
	ExtractProject(ctx context.Context, workspaceID, projectID string, opts Options) (*model.Project, error)
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/extractor/extractor.go internal/extractor/extractor_test.go
git commit -m "feat: Extractor interface — common contract for all source platform adapters"
```

---

## Task 9: Smartsheet Loader

**Files:**
- Create: `internal/loader/smartsheet/loader.go`
- Create: `internal/loader/smartsheet/loader_test.go`

**Step 1: Write failing tests**

```go
// internal/loader/smartsheet/loader_test.go
package smartsheet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/loader/smartsheet/... -v
```

Expected: FAIL

**Step 3: Create `internal/loader/smartsheet/loader.go`**

```go
package smartsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.smartsheet.com/2.0"

type Loader struct {
	token   string
	baseURL string
	client  *http.Client
}

type Option func(*Loader)

func WithBaseURL(u string) Option {
	return func(l *Loader) { l.baseURL = u }
}

func New(token string, opts ...Option) *Loader {
	l := &Loader{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
	for _, o := range opts {
		o(l)
	}
	return l
}

type columnPayload struct {
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Primary bool     `json:"primary,omitempty"`
	Options []string `json:"options,omitempty"`
}

type createSheetResponse struct {
	ResultCode int `json:"resultCode"`
	Result     struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Permalink string `json:"permalink"`
	} `json:"result"`
}

func (l *Loader) CreateSheet(ctx context.Context, proj *model.Project, workspaceID int64) (int64, error) {
	cols := make([]columnPayload, 0, len(proj.Columns))
	for i, c := range proj.Columns {
		cp := columnPayload{
			Title:   c.Name,
			Type:    transformer.ToSmartsheetColumnType(c.Type),
			Options: c.Options,
		}
		if i == 0 {
			cp.Primary = true
		}
		cols = append(cols, cp)
	}

	body, err := json.Marshal(map[string]interface{}{"name": proj.Name, "columns": cols})
	if err != nil {
		return 0, err
	}

	url := l.baseURL + "/sheets"
	if workspaceID != 0 {
		url = fmt.Sprintf("%s/workspaces/%d/sheets", l.baseURL, workspaceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("smartsheet API error: %s", resp.Status)
	}

	var result createSheetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if result.ResultCode != 0 {
		return 0, fmt.Errorf("smartsheet create sheet failed, resultCode=%d", result.ResultCode)
	}
	return result.Result.ID, nil
}

type cellPayload struct {
	ColumnID int64       `json:"columnId"`
	Value    interface{} `json:"value"`
}

type rowPayload struct {
	ToBottom bool          `json:"toBottom"`
	Cells    []cellPayload `json:"cells"`
}

func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) error {
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := l.insertRowBatch(ctx, sheetID, rows[i:end], colIndexByName); err != nil {
			return fmt.Errorf("batch %d: %w", i/batchSize, err)
		}
	}
	return nil
}

func (l *Loader) insertRowBatch(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) error {
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
		return err
	}

	url := fmt.Sprintf("%s/sheets/%d/rows", l.baseURL, sheetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("smartsheet row insert error: %s", resp.Status)
	}
	return nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/loader/smartsheet/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/loader/
git commit -m "feat: Smartsheet loader — create sheet and bulk row insert (500/batch)"
```

---

## Task 10: Asana Extractor

**Files:**
- Create: `internal/extractor/asana/asana.go`
- Create: `internal/extractor/asana/asana_test.go`
- Create: `testdata/asana/tasks.json`

**Step 1: Create fixture `testdata/asana/tasks.json`**

```json
{
  "data": [
    {
      "gid": "task_1",
      "name": "First Task",
      "notes": "Some notes",
      "completed": false,
      "due_on": "2026-04-01",
      "assignee": {"gid": "user_1", "email": "alice@example.com"},
      "tags": [{"name": "urgent"}],
      "parent": null
    }
  ]
}
```

**Step 2: Write failing tests**

```go
// internal/extractor/asana/asana_test.go
package asana_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	asanaext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/asana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsanaListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"gid": "ws_1", "name": "My Workspace"},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	workspaces, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, workspaces, 1)
	assert.Equal(t, "ws_1", workspaces[0].ID)
}

func TestAsanaExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"gid": "task_1", "name": "First Task", "notes": "notes",
					"completed": false, "due_on": "2026-04-01",
					"assignee": map[string]interface{}{"gid": "u1", "email": "a@a.com"},
					"tags": []interface{}{},
				},
			},
		})
	}))
	defer srv.Close()

	e := asanaext.New("fake-token", asanaext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "ws_1", "proj_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "proj_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Task", proj.Rows[0].Cells[0].Value)
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/extractor/asana/... -v
```

Expected: FAIL

**Step 4: Create `internal/extractor/asana/asana.go`**

```go
package asana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://app.asana.com/api/1.0"

type Extractor struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(token string, opts ...Option) *Extractor {
	e := &Extractor{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("asana"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("asana GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Data []struct {
			GID  string `json:"gid"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := e.get(ctx, "/workspaces", &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(resp.Data))
	for i, w := range resp.Data {
		ws[i] = model.Workspace{ID: w.GID, Name: w.Name}
	}
	return ws, nil
}

type asanaTask struct {
	GID       string `json:"gid"`
	Name      string `json:"name"`
	Notes     string `json:"notes"`
	Completed bool   `json:"completed"`
	DueOn     string `json:"due_on"`
	Assignee  *struct {
		GID   string `json:"gid"`
		Email string `json:"email"`
	} `json:"assignee"`
	Tags   []struct{ Name string `json:"name"` } `json:"tags"`
	Parent *struct{ GID string `json:"gid"` } `json:"parent"`
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	var resp struct{ Data []asanaTask `json:"data"` }
	path := fmt.Sprintf("/projects/%s/tasks?opt_fields=gid,name,notes,completed,due_on,assignee.email,tags.name,parent.gid", projectID)
	if err := e.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Notes", Type: model.TypeText},
		{Name: "Completed", Type: model.TypeCheckbox},
		{Name: "Due Date", Type: model.TypeDate},
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
		rows = append(rows, model.Row{ID: t.GID, ParentID: parentID, Cells: cells})
	}

	return &model.Project{ID: projectID, Name: projectID, Columns: columns, Rows: rows}, nil
}
```

**Step 5: Run tests**

```bash
go test ./internal/extractor/asana/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/extractor/asana/ testdata/asana/
git commit -m "feat: Asana extractor — list workspaces, extract tasks with fixture tests"
```

---

## Task 11: Trello Extractor

**Files:**
- Create: `internal/extractor/trello/trello.go`
- Create: `internal/extractor/trello/trello_test.go`

**Step 1: Write failing tests**

```go
// internal/extractor/trello/trello_test.go
package trello_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	trelloext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/trello"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrelloListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "org_1", "displayName": "My Org"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	workspaces, err := e.ListWorkspaces(context.Background())
	require.NoError(t, err)
	assert.Len(t, workspaces, 1)
	assert.Equal(t, "org_1", workspaces[0].ID)
}

func TestTrelloExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "card_1", "name": "First Card", "desc": "desc", "closed": false, "idList": "list_1"},
		})
	}))
	defer srv.Close()

	e := trelloext.New("key", "token", trelloext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "org_1", "board_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "board_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/trello/... -v
```

Expected: FAIL

**Step 3: Create `internal/extractor/trello/trello.go`**

```go
package trello

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.trello.com/1"

type Extractor struct {
	key     string
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(key, token string, opts ...Option) *Extractor {
	e := &Extractor{
		key: key, token: token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("trello"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	url := fmt.Sprintf("%s%s?key=%s&token=%s", e.baseURL, path, e.key, e.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("trello GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var orgs []struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}
	if err := e.get(ctx, "/members/me/organizations", &orgs); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(orgs))
	for i, o := range orgs {
		ws[i] = model.Workspace{ID: o.ID, Name: o.DisplayName}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	var cards []struct {
		ID     string  `json:"id"`
		Name   string  `json:"name"`
		Desc   string  `json:"desc"`
		Closed bool    `json:"closed"`
		Due    *string `json:"due"`
		IDList string  `json:"idList"`
	}
	if err := e.get(ctx, fmt.Sprintf("/boards/%s/cards", projectID), &cards); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "List", Type: model.TypeSingleSelect},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Closed", Type: model.TypeCheckbox},
	}

	rows := make([]model.Row, 0, len(cards))
	for _, c := range cards {
		if !opts.IncludeArchived && c.Closed {
			continue
		}
		cells := []model.Cell{
			{ColumnName: "Name", Value: c.Name},
			{ColumnName: "Description", Value: c.Desc},
			{ColumnName: "List", Value: c.IDList},
			{ColumnName: "Closed", Value: c.Closed},
		}
		if c.Due != nil {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: *c.Due})
		}
		rows = append(rows, model.Row{ID: c.ID, Cells: cells})
	}

	return &model.Project{ID: projectID, Name: projectID, Columns: columns, Rows: rows}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/trello/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/extractor/trello/
git commit -m "feat: Trello extractor — list organizations, extract board cards"
```

---

## Task 12: Jira Extractor

**Files:**
- Create: `internal/extractor/jira/jira.go`
- Create: `internal/extractor/jira/jira_test.go`
- Create: `testdata/jira/issues.json`

**Step 1: Create fixture `testdata/jira/issues.json`**

```json
{
  "issues": [
    {
      "id": "10001", "key": "PROJ-1",
      "fields": {
        "summary": "First Issue",
        "description": {
          "type": "doc",
          "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Issue body"}]}]
        },
        "status": {"name": "To Do"},
        "assignee": {"emailAddress": "dev@example.com"},
        "duedate": "2026-04-01"
      }
    }
  ],
  "total": 1
}
```

**Step 2: Write failing tests**

```go
// internal/extractor/jira/jira_test.go
package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	jiraext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/3/field" {
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "summary", "name": "Summary"},
				{"id": "status", "name": "Status"},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"id": "10001", "key": "PROJ-1",
					"fields": map[string]interface{}{
						"summary": "First Issue",
						"status":  map[string]interface{}{"name": "To Do"},
					},
				},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	e := jiraext.New("user@example.com", "api-token", srv.URL, jiraext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "PROJ", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "PROJ", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Issue", proj.Rows[0].Cells[0].Value)
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/extractor/jira/... -v
```

Expected: FAIL

**Step 4: Create `internal/extractor/jira/jira.go`**

```go
package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

type Extractor struct {
	email   string
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(email, token, instanceURL string, opts ...Option) *Extractor {
	e := &Extractor{
		email:   email,
		token:   token,
		baseURL: instanceURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("jira"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return err
	}
	creds := base64.StdEncoding.EncodeToString([]byte(e.email + ":" + e.token))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("jira GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	return []model.Workspace{{ID: e.baseURL, Name: e.baseURL}}, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectKey string, opts extractor.Options) (*model.Project, error) {
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
		path := fmt.Sprintf("/rest/api/3/search?jql=project=%s&startAt=%d&maxResults=100", projectKey, startAt)
		if err := e.get(ctx, path, &resp); err != nil {
			return nil, err
		}
		allIssues = append(allIssues, resp.Issues...)
		if startAt+len(resp.Issues) >= resp.Total {
			break
		}
		startAt += len(resp.Issues)
	}

	columns := []model.ColumnDef{
		{Name: "Summary", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "Status", Type: model.TypeSingleSelect},
		{Name: "Assignee", Type: model.TypeContact},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Key", Type: model.TypeText},
	}

	rows := make([]model.Row, 0, len(allIssues))
	for _, issue := range allIssues {
		cells := []model.Cell{{ColumnName: "Key", Value: issue.Key}}
		if v, ok := issue.Fields["summary"].(string); ok {
			cells = append(cells, model.Cell{ColumnName: "Summary", Value: v})
		}
		if desc := issue.Fields["description"]; desc != nil {
			cells = append(cells, model.Cell{ColumnName: "Description", Value: transformer.ADFToPlainText(desc)})
		}
		if status, ok := issue.Fields["status"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Status", Value: status["name"]})
		}
		if assignee, ok := issue.Fields["assignee"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Assignee", Value: assignee["emailAddress"]})
		}
		if due, ok := issue.Fields["duedate"].(string); ok {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: due})
		}
		rows = append(rows, model.Row{ID: issue.ID, Cells: cells})
	}

	return &model.Project{ID: projectKey, Name: projectKey, Columns: columns, Rows: rows}, nil
}
```

**Step 5: Run tests**

```bash
go test ./internal/extractor/jira/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/extractor/jira/ testdata/jira/
git commit -m "feat: Jira extractor — paginated JQL search with ADF description parsing"
```

---

## Task 13: Airtable Extractor

**Files:**
- Create: `internal/extractor/airtable/airtable.go`
- Create: `internal/extractor/airtable/airtable_test.go`

**Step 1: Write failing tests**

```go
// internal/extractor/airtable/airtable_test.go
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
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/airtable/... -v
```

Expected: FAIL

**Step 3: Create `internal/extractor/airtable/airtable.go`**

```go
package airtable

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.airtable.com/v0"

type Extractor struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(token string, opts ...Option) *Extractor {
	e := &Extractor{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("airtable"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, url string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("airtable GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Bases []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"bases"`
	}
	if err := e.get(ctx, "https://api.airtable.com/v0/meta/bases", &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(resp.Bases))
	for i, b := range resp.Bases {
		ws[i] = model.Workspace{ID: b.ID, Name: b.Name}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, baseID, tableID string, opts extractor.Options) (*model.Project, error) {
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

	colSet := map[string]bool{}
	for _, r := range allRecords {
		for k := range r.Fields {
			colSet[k] = true
		}
	}
	columns := make([]model.ColumnDef, 0, len(colSet))
	for name := range colSet {
		columns = append(columns, model.ColumnDef{Name: name, Type: model.TypeText})
	}

	rows := make([]model.Row, 0, len(allRecords))
	for _, r := range allRecords {
		cells := make([]model.Cell, 0, len(r.Fields))
		for k, v := range r.Fields {
			cells = append(cells, model.Cell{ColumnName: k, Value: fmt.Sprintf("%v", v)})
		}
		rows = append(rows, model.Row{ID: r.ID, Cells: cells})
	}

	return &model.Project{ID: tableID, Name: tableID, Columns: columns, Rows: rows}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/airtable/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/extractor/airtable/
git commit -m "feat: Airtable extractor — paginated record fetch with dynamic column inference"
```

---

## Task 14: Notion Extractor

**Files:**
- Create: `internal/extractor/notion/notion.go`
- Create: `internal/extractor/notion/notion_test.go`

**Step 1: Write failing tests**

```go
// internal/extractor/notion/notion_test.go
package notion_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	notionext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/notion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotionExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"id": "page_1",
					"properties": map[string]interface{}{
						"Name": map[string]interface{}{
							"title": []map[string]interface{}{{"plain_text": "First Page"}},
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
	assert.Equal(t, "db_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/notion/... -v
```

Expected: FAIL

**Step 3: Create `internal/extractor/notion/notion.go`**

```go
package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.notion.com/v1"
const notionVersion = "2022-06-28"

type Extractor struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(token string, opts ...Option) *Extractor {
	e := &Extractor{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("notion"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) post(ctx context.Context, path string, body interface{}, out interface{}) error {
	e.rl.Wait()
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notion POST %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Results []struct {
			ID    string `json:"id"`
			Title []struct {
				PlainText string `json:"plain_text"`
			} `json:"title"`
		} `json:"results"`
	}
	body := map[string]interface{}{"filter": map[string]interface{}{"value": "database", "property": "object"}}
	if err := e.post(ctx, "/search", body, &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, 0, len(resp.Results))
	for _, r := range resp.Results {
		name := r.ID
		if len(r.Title) > 0 {
			name = r.Title[0].PlainText
		}
		ws = append(ws, model.Workspace{ID: r.ID, Name: name})
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, databaseID string, opts extractor.Options) (*model.Project, error) {
	var allPages []map[string]interface{}
	var cursor *string

	for {
		body := map[string]interface{}{"page_size": 100}
		if cursor != nil {
			body["start_cursor"] = *cursor
		}
		var resp struct {
			Results    []map[string]interface{} `json:"results"`
			HasMore    bool                     `json:"has_more"`
			NextCursor *string                  `json:"next_cursor"`
		}
		if err := e.post(ctx, fmt.Sprintf("/databases/%s/query", databaseID), body, &resp); err != nil {
			return nil, err
		}
		allPages = append(allPages, resp.Results...)
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}

	colSet := map[string]bool{}
	for _, p := range allPages {
		if props, ok := p["properties"].(map[string]interface{}); ok {
			for k := range props {
				colSet[k] = true
			}
		}
	}
	columns := make([]model.ColumnDef, 0, len(colSet))
	for name := range colSet {
		columns = append(columns, model.ColumnDef{Name: name, Type: model.TypeText})
	}

	rows := make([]model.Row, 0, len(allPages))
	for _, p := range allPages {
		id, _ := p["id"].(string)
		props, ok := p["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		cells := make([]model.Cell, 0)
		for colName, propVal := range props {
			if value := extractNotionPropValue(propVal); value != "" {
				cells = append(cells, model.Cell{ColumnName: colName, Value: value})
			}
		}
		rows = append(rows, model.Row{ID: id, Cells: cells})
	}

	return &model.Project{ID: databaseID, Name: databaseID, Columns: columns, Rows: rows}, nil
}

func extractNotionPropValue(prop interface{}) string {
	m, ok := prop.(map[string]interface{})
	if !ok {
		return ""
	}
	if title, ok := m["title"].([]interface{}); ok && len(title) > 0 {
		if rt, ok := title[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rt["plain_text"])
		}
	}
	if sel, ok := m["select"].(map[string]interface{}); ok {
		return fmt.Sprintf("%v", sel["name"])
	}
	if date, ok := m["date"].(map[string]interface{}); ok {
		return fmt.Sprintf("%v", date["start"])
	}
	if cb, ok := m["checkbox"]; ok {
		return fmt.Sprintf("%v", cb)
	}
	if num, ok := m["number"]; ok && num != nil {
		return fmt.Sprintf("%v", num)
	}
	if rt, ok := m["rich_text"].([]interface{}); ok && len(rt) > 0 {
		if rtm, ok := rt[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rtm["plain_text"])
		}
	}
	return ""
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/notion/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/extractor/notion/
git commit -m "feat: Notion extractor — database query pagination with property type extraction"
```

---

## Task 15: Monday.com Extractor

**Files:**
- Create: `internal/extractor/monday/monday.go`
- Create: `internal/extractor/monday/monday_test.go`

**Step 1: Write failing tests**

```go
// internal/extractor/monday/monday_test.go
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
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/monday/... -v
```

Expected: FAIL

**Step 3: Create `internal/extractor/monday/monday.go`**

```go
package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.monday.com/v2"

type Extractor struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(token string, opts ...Option) *Extractor {
	e := &Extractor{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("monday"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) query(ctx context.Context, q string, out interface{}) error {
	e.rl.Wait()
	body, err := json.Marshal(map[string]string{"query": q})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", e.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Version", "2024-01")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var wrapper struct {
		Data   json.RawMessage `json:"data"`
		Errors []interface{}   `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return err
	}
	if len(wrapper.Errors) > 0 {
		return fmt.Errorf("monday.com error: %v", wrapper.Errors[0])
	}
	return json.Unmarshal(wrapper.Data, out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var data struct {
		Workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workspaces"`
	}
	if err := e.query(ctx, `{ workspaces { id name } }`, &data); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(data.Workspaces))
	for i, w := range data.Workspaces {
		ws[i] = model.Workspace{ID: w.ID, Name: w.Name}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, boardID string, opts extractor.Options) (*model.Project, error) {
	var data struct {
		Boards []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Columns []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"columns"`
			ItemsPage struct {
				Items []struct {
					ID           string `json:"id"`
					Name         string `json:"name"`
					ColumnValues []struct {
						ID   string `json:"id"`
						Text string `json:"text"`
					} `json:"column_values"`
				} `json:"items"`
			} `json:"items_page"`
		} `json:"boards"`
	}

	q := fmt.Sprintf(`{ boards(ids: [%s]) { id name columns { id title type } items_page(limit: 500) { items { id name column_values { id text } } } } }`, boardID)
	if err := e.query(ctx, q, &data); err != nil {
		return nil, err
	}
	if len(data.Boards) == 0 {
		return nil, fmt.Errorf("board %s not found", boardID)
	}
	board := data.Boards[0]

	columns := make([]model.ColumnDef, 0, len(board.Columns))
	for _, c := range board.Columns {
		colType := model.TypeText
		switch c.Type {
		case "color", "status":
			colType = model.TypeSingleSelect
		case "date":
			colType = model.TypeDate
		case "checkbox":
			colType = model.TypeCheckbox
		case "people", "multiple-person":
			colType = model.TypeMultiContact
		}
		columns = append(columns, model.ColumnDef{Name: c.Title, Type: colType})
	}

	rows := make([]model.Row, 0, len(board.ItemsPage.Items))
	for _, item := range board.ItemsPage.Items {
		cells := []model.Cell{{ColumnName: "Name", Value: item.Name}}
		for _, cv := range item.ColumnValues {
			if text := transformer.StripHTML(cv.Text); text != "" {
				cells = append(cells, model.Cell{ColumnName: cv.ID, Value: text})
			}
		}
		rows = append(rows, model.Row{ID: item.ID, Cells: cells})
	}

	return &model.Project{ID: boardID, Name: board.Name, Columns: columns, Rows: rows}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/monday/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/extractor/monday/
git commit -m "feat: Monday.com extractor — GraphQL board+items query with HTML stripping"
```

---

## Task 16: Wrike Extractor

**Files:**
- Create: `internal/extractor/wrike/wrike.go`
- Create: `internal/extractor/wrike/wrike_test.go`

**Step 1: Write failing tests**

```go
// internal/extractor/wrike/wrike_test.go
package wrike_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	wrikeext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/wrike"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrikeExtractProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id": "IEABCDE", "title": "First Task",
					"description": "<p>Task description</p>",
					"status":      "Active",
					"dates":       map[string]interface{}{"due": "2026-04-01"},
				},
			},
		})
	}))
	defer srv.Close()

	e := wrikeext.New("fake-token", wrikeext.WithBaseURL(srv.URL))
	proj, err := e.ExtractProject(context.Background(), "", "folder_1", extractor.Options{})
	require.NoError(t, err)
	assert.Equal(t, "folder_1", proj.ID)
	assert.Len(t, proj.Rows, 1)
	assert.Equal(t, "First Task", proj.Rows[0].Cells[0].Value)
	assert.Equal(t, "Task description", proj.Rows[0].Cells[1].Value)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/extractor/wrike/... -v
```

Expected: FAIL

**Step 3: Create `internal/extractor/wrike/wrike.go`**

```go
package wrike

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://www.wrike.com/api/v4"

type Extractor struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(token string, opts ...Option) *Extractor {
	e := &Extractor{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("wrike"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("wrike GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := e.get(ctx, "/accounts", &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(resp.Data))
	for i, a := range resp.Data {
		ws[i] = model.Workspace{ID: a.ID, Name: a.Name}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, folderID string, opts extractor.Options) (*model.Project, error) {
	var resp struct {
		Data []struct {
			ID          string   `json:"id"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Status      string   `json:"status"`
			ParentIDs   []string `json:"parentIds"`
			Dates       struct {
				Due string `json:"due"`
			} `json:"dates"`
		} `json:"data"`
	}
	if err := e.get(ctx, fmt.Sprintf("/folders/%s/tasks?fields=[\"description\",\"dates\",\"parentIds\"]", folderID), &resp); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Title", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "Status", Type: model.TypeSingleSelect},
		{Name: "Due Date", Type: model.TypeDate},
	}

	rows := make([]model.Row, 0, len(resp.Data))
	for _, t := range resp.Data {
		parentID := ""
		if len(t.ParentIDs) > 0 {
			parentID = t.ParentIDs[0]
		}
		cells := []model.Cell{
			{ColumnName: "Title", Value: t.Title},
			{ColumnName: "Description", Value: transformer.StripHTML(t.Description)},
			{ColumnName: "Status", Value: t.Status},
		}
		if t.Dates.Due != "" {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: t.Dates.Due})
		}
		rows = append(rows, model.Row{ID: t.ID, ParentID: parentID, Cells: cells})
	}

	return &model.Project{ID: folderID, Name: folderID, Columns: columns, Rows: rows}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/extractor/wrike/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/extractor/wrike/
git commit -m "feat: Wrike extractor — folder tasks with HTML description stripping"
```

---

## Task 17: Preview / Dry-Run Summary

**Files:**
- Create: `internal/preview/preview.go`
- Create: `internal/preview/preview_test.go`

**Step 1: Write failing tests**

```go
// internal/preview/preview_test.go
package preview_test

import (
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/preview"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestSummarize(t *testing.T) {
	workspaces := []model.Workspace{
		{
			ID: "ws1",
			Projects: []model.Project{
				{
					Columns: []model.ColumnDef{{Name: "A"}, {Name: "B"}},
					Rows: []model.Row{
						{
							Cells:       []model.Cell{{}, {}},
							Attachments: []model.Attachment{{SizeBytes: 1024}, {SizeBytes: 30 * 1024 * 1024}},
							Comments:    []model.Comment{{}, {}},
						},
						{Cells: []model.Cell{{}, {}}},
					},
				},
			},
		},
	}

	s := preview.Summarize(workspaces)
	assert.Equal(t, 1, s.Workspaces)
	assert.Equal(t, 1, s.Sheets)
	assert.Equal(t, 2, s.Rows)
	assert.Equal(t, 2, s.Columns)
	assert.Equal(t, 2, s.Attachments)
	assert.Equal(t, 2, s.Comments)
	assert.Equal(t, 1, s.OversizedAttachments)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/preview/... -v
```

Expected: FAIL

**Step 3: Create `internal/preview/preview.go`**

```go
package preview

import (
	"fmt"

	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const maxAttachmentBytes = 25 * 1024 * 1024

type Summary struct {
	Workspaces           int
	Sheets               int
	Rows                 int
	Columns              int
	Attachments          int
	Comments             int
	OversizedAttachments int
	Warnings             []string
}

func Summarize(workspaces []model.Workspace) Summary {
	s := Summary{Workspaces: len(workspaces)}
	for _, ws := range workspaces {
		s.Sheets += len(ws.Projects)
		for _, proj := range ws.Projects {
			s.Columns += len(proj.Columns)
			s.Rows += len(proj.Rows)
			for _, row := range proj.Rows {
				s.Attachments += len(row.Attachments)
				s.Comments += len(row.Comments)
				for _, att := range row.Attachments {
					if att.SizeBytes > maxAttachmentBytes {
						s.OversizedAttachments++
					}
				}
			}
		}
	}
	if s.OversizedAttachments > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%d attachment(s) exceed 25MB and will be skipped", s.OversizedAttachments))
	}
	return s
}
```

**Step 4: Run tests**

```bash
go test ./internal/preview/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/preview/
git commit -m "feat: preview — summarize migration counts and oversized attachment warnings"
```

---

## Task 18: Wire CLI — run.go

**Files:**
- Create: `cmd/migrate/run.go`

**Step 1: Verify existing CLI compiles**

```bash
go build ./cmd/migrate/
./migrate-to-smartsheet --help
```

Expected: help output with all flags

**Step 2: Create `cmd/migrate/run.go`**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	survey "github.com/AlecAivazis/survey/v2"
	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	asanaext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/asana"
	airtableext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/airtable"
	jiraext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/jira"
	mondayext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/monday"
	notionext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/notion"
	trelloext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/trello"
	wrikeext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/wrike"
	ssloader "github.com/bchauhan/migrate-to-smartsheet/internal/loader/smartsheet"
	"github.com/bchauhan/migrate-to-smartsheet/internal/preview"
	"github.com/bchauhan/migrate-to-smartsheet/internal/state"
	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
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
	includeAttachments, _ := flags.GetBool("include-attachments")
	includeComments, _ := flags.GetBool("include-comments")
	includeArchived, _ := flags.GetBool("include-archived")

	if sourceStr == "" {
		survey.AskOne(&survey.Select{
			Message: "Select source platform:",
			Options: []string{"asana", "monday", "trello", "jira", "airtable", "notion", "wrike"},
		}, &sourceStr)
	}
	if sourceToken == "" {
		survey.AskOne(&survey.Password{Message: fmt.Sprintf("[%s] API token:", sourceStr)}, &sourceToken)
	}

	ext, err := buildExtractor(sourceStr, sourceToken, cmd)
	if err != nil {
		return fmt.Errorf("building extractor: %w", err)
	}

	fmt.Print("  Connecting... ")
	workspaces, err := ext.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("listing workspaces: %w", err)
	}
	fmt.Println("✓")

	if ssToken == "" {
		survey.AskOne(&survey.Password{Message: "Smartsheet API token:"}, &ssToken)
	}
	loader := ssloader.New(ssToken)

	stateFile := fmt.Sprintf(".migrate-state-%s.json", time.Now().Format("2006-01-02"))
	var migState *state.MigrationState
	if existing, err := state.Load(stateFile); err == nil {
		var resume bool
		survey.AskOne(&survey.Confirm{
			Message: fmt.Sprintf("Found incomplete migration from %s. Resume?", existing.StartedAt.Format("2006-01-02")),
			Default: true,
		}, &resume)
		if resume {
			migState = existing
		}
	}
	if migState == nil {
		migState = &state.MigrationState{Source: sourceStr, StartedAt: time.Now().UTC()}
	}

	var userMap *transformer.UserMap
	if userMapPath != "" {
		f, err := os.Open(userMapPath)
		if err != nil {
			return fmt.Errorf("opening user map: %w", err)
		}
		defer f.Close()
		userMap, err = transformer.LoadUserMapFromReader(f)
		if err != nil {
			return fmt.Errorf("loading user map: %w", err)
		}
	}
	_ = userMap

	opts := extractor.Options{
		IncludeAttachments: includeAttachments,
		IncludeComments:    includeComments,
		IncludeArchived:    includeArchived,
	}

	fmt.Println("\n  Analyzing source data...")
	var allProjects []model.Project
	for _, ws := range workspaces {
		for _, proj := range ws.Projects {
			if migState.IsCompleted(proj.ID) {
				fmt.Printf("  ↻ Skipping already-migrated: %s\n", proj.Name)
				continue
			}
			extracted, err := ext.ExtractProject(ctx, ws.ID, proj.ID, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠  Skipping project %s: %v\n", proj.Name, err)
				continue
			}
			allProjects = append(allProjects, *extracted)
		}
	}

	summary := preview.Summarize(workspaces)
	fmt.Printf(`
╔══════════════════════════╗
║    Migration Preview     ║
╠══════════════════════════╣
║ Workspaces:    %-9d║
║ Sheets:        %-9d║
║ Rows:          %-9d║
║ Columns:       %-9d║
║ Attachments:   %-9d║
║ Comments:      %-9d║
║ Warnings:      %-9d║
╚══════════════════════════╝
`, summary.Workspaces, summary.Sheets, summary.Rows, summary.Columns,
		summary.Attachments, summary.Comments, len(summary.Warnings))

	for _, w := range summary.Warnings {
		fmt.Fprintf(os.Stderr, "  ⚠  %s\n", w)
	}

	if !yes {
		var proceed bool
		survey.AskOne(&survey.Confirm{Message: "Proceed with migration?", Default: false}, &proceed)
		if !proceed {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	bar := progressbar.Default(int64(len(allProjects)), "Migrating")
	for _, proj := range allProjects {
		sheetID, err := loader.CreateSheet(ctx, &proj, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to create sheet %s: %v\n", proj.Name, err)
			continue
		}
		if err := loader.BulkInsertRows(ctx, sheetID, proj.Rows, map[string]int64{}); err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to insert rows for %s: %v\n", proj.Name, err)
		}
		migState.MarkCompleted(proj.ID)
		_ = state.Save(stateFile, migState)
		bar.Add(1)
	}

	fmt.Println("\n✓ Migration complete!")
	_ = os.Remove(stateFile)
	return nil
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

**Step 3: Build and run all tests**

```bash
go build ./cmd/migrate/
go test ./... -v
```

Expected: builds cleanly, all tests PASS

**Step 4: Commit**

```bash
git add cmd/migrate/run.go
git commit -m "feat: wire CLI — interactive/non-interactive flow, extractor dispatch, preview, migrate loop"
```

---

## Task 19: CI/CD Pipeline

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Create: `.goreleaser.yaml`

**Step 1: Create `.github/workflows/ci.yml`**

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go mod download
      - run: go test ./... -v -race -coverprofile=coverage.out
      - uses: codecov/codecov-action@v4
        with:
          file: coverage.out
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest
```

**Step 2: Create `.github/workflows/release.yml`**

```yaml
name: Release
on:
  push:
    tags:
      - "v*"
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**Step 3: Create `.goreleaser.yaml`**

```yaml
version: 2
before:
  hooks:
    - go mod tidy
    - go test ./...
builds:
  - id: migrate-to-smartsheet
    main: ./cmd/migrate/
    binary: migrate-to-smartsheet
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
archives:
  - id: default
    format_overrides:
      - goos: windows
        format: zip
    files:
      - README.md
checksum:
  name_template: "checksums.txt"
changelog:
  sort: asc
  filters:
    exclude: ["^docs:", "^test:", "^chore:"]
release:
  github:
    owner: bchauhan
    name: migrate-to-smartsheet
```

**Step 4: Commit**

```bash
git add .github/ .goreleaser.yaml
git commit -m "ci: GitHub Actions CI (test+lint) and GoReleaser release pipeline"
```

---

## Task 20: Final Verification

**Step 1: Run full test suite**

```bash
go test ./... -race -count=1
```

Expected: all PASS

**Step 2: Build all target platforms**

```bash
GOOS=linux   GOARCH=amd64 go build -o /dev/null ./cmd/migrate/
GOOS=darwin  GOARCH=arm64 go build -o /dev/null ./cmd/migrate/
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/migrate/
```

Expected: all compile with no errors

**Step 3: Verify binary**

```bash
go run ./cmd/migrate/ --help
```

Expected: full help output listing all flags

**Step 4: Tag for release**

```bash
git tag v0.1.0
git push origin main --tags
```

This triggers the GoReleaser workflow and publishes binaries to GitHub Releases.
