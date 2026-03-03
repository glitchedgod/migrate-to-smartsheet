# migrate-to-smartsheet — Complete Rework Design

**Date:** 2026-03-03
**Status:** Approved
**Scope:** Full systematic fix of all critical bugs, missing capabilities, and data fidelity gaps identified in code review.

---

## Why a Rework

A thorough code review found the tool currently migrates zero data due to a fundamental loop bug. Additionally, all 7 extractors are missing significant fields, value transforms are absent, and attachment/comment wiring is incomplete. This doc covers every fix needed for a production-quality v1.0 migration.

---

## Core Architecture Change: `ListProjects`

The `Extractor` interface gains a third method:

```go
type ProjectRef struct {
    ID   string
    Name string
}

type Extractor interface {
    ListWorkspaces(ctx context.Context) ([]model.Workspace, error)
    ListProjects(ctx context.Context, workspaceID string) ([]ProjectRef, error)
    ExtractProject(ctx context.Context, workspaceID, projectID string, opts Options) (*model.Project, error)
}
```

`run.go` calls `ListWorkspaces` → user selects workspace → `ListProjects(workspaceID)` → user multi-selects projects → `ExtractProject` per selection. The `ws.Projects` iteration is removed.

---

## Fixed Migration Loop (run.go)

```
1. Prompt/flag: source platform + token
2. ListWorkspaces → interactive select (or --workspace flag)
3. ListProjects(workspaceID) → interactive multi-select (or --projects flag)
4. Prompt/flag: Smartsheet token + target workspace
5. Load user map if --user-map provided
6. For each selected project:
   a. Check state: skip if already completed
   b. ExtractProject → model.Project (fully populated)
   c. Apply --exclude-fields filter to columns + cells
   d. Apply value transforms (dates, contacts, booleans, user remapping)
   e. Apply sheet naming (prefix/suffix/template)
   f. Check conflict (skip/rename/overwrite existing sheet)
   g. CreateSheet(proj, targetWorkspaceID) → (sheetID, colMap)
   h. BulkInsertRows(sheetID, rows, colMap) → (sourceID→ssRowID map)
   i. If --include-attachments: download + UploadAttachment per row
   j. If --include-comments: AddComment per row discussion
   k. MarkCompleted(proj.ID), save state
7. Remove state file only if all projects succeeded
```

---

## Extractor Fixes

### All Extractors
- Add `ListProjects(ctx, workspaceID) ([]ProjectRef, error)`
- Set `proj.Name` from the API-returned name (not raw ID)
- Apply `opts.CreatedAfter` / `opts.UpdatedAfter` as API query filters where supported
- Populate `model.Row.Attachments` when `opts.IncludeAttachments = true`
- Populate `model.Row.Comments` when `opts.IncludeComments = true`

### Asana
- `ListProjects`: `GET /workspaces/{id}/projects?opt_fields=gid,name`
- Custom fields: `GET /projects/{id}/custom_field_settings` → build typed `ColumnDef` per field
- Subtask recursion: `GET /tasks/{id}/subtasks` for tasks with `num_subtasks > 0`
- Attachments: `GET /tasks/{id}/attachments`, download `download_url` immediately (expires 2min)
- Comments: `GET /tasks/{id}/stories?opt_fields=type,text,created_by.email,created_at` filter `type=comment`
- Add `created_at`, `modified_at`, `start_on` to opt_fields

### Monday.com
- `ListProjects`: `{ boards(workspace_ids: [ID]) { id name } }`
- Fix column name/ID mismatch: build `colTitleByID map[string]string` from board columns; use title as `ColumnName` in cells
- Subitems: query `subitems { id name column_values { id text } }` per item
- Board groups: `groups { id title }` → add "Group" `SingleSelect` column
- Attachments: `assets { id url name file_size }` per item via `GET /items/{id}/assets`
- Comments/updates: `updates { id text_body created_at creator { email } }` per item
- Use `column_values { id text value }` — parse `value` JSON for richer data (email for people, ISO date for dates)
- 10k item shard: if `items_count > 10000`, split into multiple sheets named `"Board (1 of N)"`

### Trello
- `ListProjects`: `GET /organizations/{id}/boards?fields=id,name`
- List names: `GET /boards/{id}/lists?fields=id,name` → build ID→name map, replace `idList` cell value
- Members: `GET /cards/{id}/members` → Contact column
- Labels: labels array → MultiSelect column
- Checklists: `GET /cards/{id}/checklists` → child rows per checklist item
- Attachments: `GET /cards/{id}/attachments` → download non-trello-uploaded ones
- Comments: `GET /cards/{id}/actions?filter=commentCard`
- Add `limit=1000` to cards request, handle multiple pages with `before` cursor
- Board name: `GET /boards/{id}?fields=name`

### Jira
- `ListProjects`: `GET /rest/api/3/project/search?expand=description`
- Priority, issue type, reporter: add to `opt_fields` and column list
- Labels: `string[]` field → MultiSelect column
- Custom fields: use `/rest/api/3/field` metadata already fetched; for each `customfield_*` key present in issues, map to canonical type based on `schema.type` (`string`→Text, `date`→Date, `datetime`→DateTime, `number`→Number, `array`→MultiSelect, `user`→Contact)
- Epic/parent link: check `parent.id` (next-gen) and `customfield_10014` (classic epic link)
- Sprint: extract from `customfield_10020[0].name`
- Attachments: `GET /rest/api/3/issue/{key}/attachments` with Basic auth header
- Comments: `GET /rest/api/3/issue/{key}/comment`
- Date filter in JQL: append `AND created >= "YYYY-MM-DD"` / `AND updated >= "YYYY-MM-DD"` when opts set
- ADF: add `table`, `tableRow`, `tableCell`, `hardBreak`, `mention`, `inlineCard`, `emoji`, `rule` node handling

### Airtable
- `ListProjects`: `GET /meta/bases/{baseId}/tables?include=fields` — returns tables with full field schema
- Column types: map Airtable field types to canonical types:
  - `singleLineText`, `multilineText`, `url`, `email`, `phoneNumber` → Text
  - `number`, `currency`, `percent`, `duration` → Number
  - `date` → Date
  - `dateTime` → DateTime
  - `checkbox` → Checkbox
  - `singleSelect` → SingleSelect (with options)
  - `multipleSelects` → MultiSelect (with options)
  - `singleCollaborator`, `createdBy`, `lastModifiedBy` → Contact
  - `multipleCollaborators` → MultiContact
  - `multipleAttachments` → extract as Attachment objects
  - `multipleRecordLinks` → Text (resolve display names)
  - `formula`, `rollup`, `lookup`, `count` → Text (snapshot value)
  - `autoNumber`, `createdTime`, `lastModifiedTime` → Text (system field)
- Attachment download: for `multipleAttachments` fields, download each file from `url` immediately (expires 2h)
- `multipleSelects` value: extract as `[]string`, not `fmt.Sprintf`
- Collaborator value: extract `.email` field

### Notion
- `ListProjects`: paginated `POST /search` with `filter: {value: "database"}` — follow pagination
- All property types in `extractNotionPropValue`:
  - `multi_select`: extract names as `[]string`
  - `people`: extract emails as `[]string` → MultiContact
  - `url`: extract string → Text
  - `email`: extract string → Text
  - `phone_number`: extract string → Text
  - `created_time` / `last_edited_time`: extract ISO timestamp → DateTime
  - `created_by` / `last_edited_by`: extract `person.email` → Contact
  - `status`: extract `status.name` → SingleSelect
  - `relation`: extract linked page IDs or titles → Text
  - `formula`: extract `formula.string` / `formula.number` / `formula.boolean` / `formula.date.start`
  - `rollup`: extract `rollup.array` items or scalar
  - `unique_id`: extract `unique_id.prefix` + `unique_id.number` as string
  - `files`: extract as Attachment objects (download immediately, expires 1h)
  - `date` with end: format as `"YYYY-MM-DD – YYYY-MM-DD"`
- Page body: when `opts.IncludeComments` (repurposed or new flag `IncludePageBody`), call `GET /blocks/{pageId}/children` and convert block content to plain text, store in a "Page Body" column
- Database title: fetch from database object, set `proj.Name`
- Column types: map property types to canonical types correctly

### Wrike
- `ListProjects`: `GET /folders/{workspaceId}/folders?fields=["project"]` — filter `folder.project != null`
- Assignees: `responsibles` array → resolve user IDs to emails via `GET /users/{userId}` → MultiContact
- Custom fields: `GET /customfields` to build GUID→definition map; extract `customFields` array from tasks
- Start date: `dates.start` field
- Attachments: `GET /folders/{folderId}/attachments` or `GET /tasks/{taskId}/attachments`
- Comments: `GET /tasks/{taskId}/comments`
- Folder/project name: `GET /folders/{folderId}?fields=["title"]`
- URL-encode all query params properly using `url.Values`
- Sequential folder ops: document + enforce with mutex if parallelism is ever added

---

## Loader Fixes

### BulkInsertRows — returns row ID map
```go
func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) (map[string]int64, error)
// returns: sourceRowID → smartsheetRowID
```
Parse the Smartsheet response body to extract created row IDs and map them back to source IDs (matched by position, since Smartsheet returns rows in insert order).

### Parent/child rows — two-pass insert
1. First pass: insert top-level rows (`ParentID == ""`), capture `sourceID→ssID` map
2. Second pass: insert child rows with `parentId` set to the Smartsheet ID of their parent

### Sheet naming
Apply `--sheet-prefix`, `--sheet-suffix`, `--sheet-name-template` to `proj.Name` before `CreateSheet`.

### Conflict handling
Before `CreateSheet`, call `GET /workspaces/{id}/sheets` to check if a sheet with the same name exists:
- `skip`: log warning, continue to next project
- `rename`: append ` (YYYY-MM-DD HH:MM)` to sheet name
- `overwrite`: delete existing sheet first

### Smartsheet rate limiter
Add `rl *ratelimit.Limiter` to `Loader`, call `l.rl.Wait()` before every HTTP request.

### Workspace resolution
Accept `targetWorkspaceID int64` in `CreateSheet`. Resolve from `--smartsheet-workspace` flag by calling `GET /workspaces` and matching by name.

---

## Value Transforms (new `internal/transformer/values.go`)

```go
// NormalizeDate converts any date/datetime string to YYYY-MM-DD for DATE columns
func NormalizeDate(v interface{}) string

// FormatContact converts email string to Smartsheet CONTACT_LIST cell value
// Smartsheet expects: {"email": "..."}
func FormatContact(email string) map[string]string

// FormatMultiSelect converts []string to comma-separated string for MULTI_PICKLIST
func FormatMultiSelect(values []string) string

// FormatBool converts "true"/"false" string or bool to actual bool
func FormatBool(v interface{}) bool

// ApplyUserRemap remaps a contact cell value through the user map
func ApplyUserRemap(v interface{}, um *UserMap) interface{}

// TransformCellValue applies all appropriate transforms based on column type
func TransformCellValue(v interface{}, colType model.ColumnType, um *UserMap) interface{}
```

`TransformCellValue` is called on every cell in `run.go` before `BulkInsertRows`.

---

## State Improvements

- State file: `.migrate-state-{source}-{unix_timestamp}.json` — no same-day collision
- After each 500-row batch: update `migState.PartialSheet.LastCompletedRow`, save state
- On resume: `BulkInsertRows` accepts `startRow int` offset, skips already-inserted rows
- Delete state file only when **all** projects completed successfully (no errors)
- Persist `--projects` filter in state so resume uses same scope

---

## Testing Strategy

Each extractor change gets:
1. Expanded httptest fixture covering the new fields
2. Tests for each new property type / field type
3. Tests for attachment/comment paths
4. Integration test runner: `go test ./... -tags=integration -run TestIntegration` (skipped in CI without env vars set)

Value transform tests: table-driven tests covering every (type, input) → expected output combination.

---

## Non-Goals (still v1.0)

- No report/dashboard migration
- No bidirectional sync
- No formula logic migration (snapshot values only)
- No Notion page body migration (blocks) unless trivially available

---

## Implementation Order

1. Value transforms (`transformer/values.go`) — no dependencies
2. `Extractor` interface + `ListProjects` on all 7 platforms
3. `run.go` loop rewrite — depends on 1+2
4. `BulkInsertRows` row ID return + parent/child two-pass — depends on 3
5. Extractor field completeness — parallelisable per platform after 2
6. Attachment wiring (download + upload) — depends on 4
7. Comment wiring — depends on 4
8. Loader: naming, conflict, workspace, rate limiter — parallelisable after 3
9. State improvements — depends on 3+4
10. Tests for all of the above
