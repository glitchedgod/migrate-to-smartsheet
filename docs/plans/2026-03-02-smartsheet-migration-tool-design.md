# Smartsheet Migration Tool — Design Document

**Date:** 2026-03-02
**Status:** Approved
**Language:** Go
**Interface:** CLI (interactive + non-interactive flags)
**Distribution:** Single binary via GitHub Releases + GoReleaser

---

## Overview

A command-line tool that migrates data from Smartsheet competitor platforms into Smartsheet. Non-destructive (source data is never modified). Resumable (state file tracks progress). Cross-platform single binary.

**Supported sources at v1.0:**
- Asana (REST)
- Monday.com (GraphQL)
- Trello (REST)
- Jira (REST — Jira Cloud v3)
- Airtable (REST)
- Notion (REST)
- Wrike (REST v4)

---

## Architecture

### Pattern: Monolithic CLI with embedded adapters

Single Go binary. All source adapters compiled in. Common `Extractor` interface per source platform. Central canonical model, transformer, and loader pipeline.

```
Source Platform API
       │
       ▼
  Extractor (per-platform)
       │  reads source objects, paginates, respects rate limits
       ▼
  Canonical Model (pkg/model)
       │  platform-neutral intermediate representation
       ▼
  Transformer
       │  field type mapping, rich text stripping, user remapping,
       │  field exclusions, naming conventions
       ▼
  Loader (Smartsheet writer)
       │  bulk row inserts, attachment upload, comments
       ▼
  Smartsheet API
```

### Repository Structure

```
migrate-to-smartsheet/
├── cmd/migrate/           # main.go — cobra CLI entry point
├── internal/
│   ├── extractor/
│   │   ├── extractor.go   # Extractor interface
│   │   ├── asana/
│   │   ├── monday/        # GraphQL client
│   │   ├── trello/
│   │   ├── jira/
│   │   ├── airtable/
│   │   ├── notion/
│   │   └── wrike/
│   ├── loader/
│   │   └── smartsheet/
│   ├── transformer/
│   │   ├── fieldmap.go    # Canonical type → Smartsheet column type
│   │   ├── richtext.go    # ADF / HTML / Markdown → plain text
│   │   └── usermap.go     # Source user ID → Smartsheet email
│   ├── state/             # Resume state (JSON file)
│   ├── ratelimit/         # Per-platform token buckets
│   └── preview/           # Pre-migration summary/counts
├── pkg/model/             # Canonical intermediate types (exported)
├── testdata/              # Fixture JSON from each platform's API
├── docs/plans/
├── .github/workflows/
│   ├── ci.yml
│   └── release.yml
├── .goreleaser.yaml
├── go.mod
└── README.md
```

---

## Canonical Data Model

### Hierarchy

```
Workspace
└── Project  →  Smartsheet Sheet
    ├── ColumnDef[]  (schema)
    └── Row[]
        ├── Cell[]
        ├── Attachment[]
        └── Comment[]
```

### Canonical Column Types → Smartsheet Mapping

| Canonical Type  | Smartsheet Column Type  | Source Platforms                                        |
|-----------------|-------------------------|---------------------------------------------------------|
| `Text`          | `TEXT_NUMBER`           | All platforms                                           |
| `Number`        | `TEXT_NUMBER`           | All platforms                                           |
| `Date`          | `DATE`                  | All platforms                                           |
| `DateTime`      | `DATETIME`              | Jira, Notion, Asana                                     |
| `Checkbox`      | `CHECKBOX`              | All platforms                                           |
| `SingleSelect`  | `PICKLIST`              | All platforms                                           |
| `MultiSelect`   | `MULTI_PICKLIST`        | Asana multi-enum, Monday tags/dropdown, Airtable, Notion|
| `Contact`       | `CONTACT_LIST`          | Asana assignee, Monday people, Jira assignee, Wrike     |
| `MultiContact`  | `MULTI_CONTACT_LIST`    | Asana followers, Monday multi-people, Jira multi-user  |
| `URL`           | `TEXT_NUMBER`           | Jira URL field, Notion URL property                     |
| `Duration`      | `TEXT_NUMBER` (string)  | Jira worklog, Airtable duration, Wrike duration         |

### Fields That Cannot Map Cleanly (Explicit Handling)

| Source Feature                        | Migration Behavior                                                    |
|---------------------------------------|-----------------------------------------------------------------------|
| Formula / Rollup / Lookup fields      | Static value snapshot as `TEXT_NUMBER`. Warning logged.              |
| Subtask hierarchy (deep nesting)      | Parent/child rows via `parentId`. Depth beyond Smartsheet limit flattened. |
| Multi-parent items (Asana, Wrike)     | Canonical parent = first/primary project. Additional memberships logged. |
| Rich text (Jira ADF, Wrike HTML, Notion blocks) | Stripped to plain text.                                   |
| System fields (auto-number, created-by, etc.) | Read-only snapshot as `TEXT_NUMBER`.                        |
| Computed fields (mirror, progress, etc.) | Snapshot value as `TEXT_NUMBER`. Warning logged.               |
| Monday.com boards > 10,000 items      | Auto-sharded into multiple sheets: `"Board Name (1 of 3)"`.         |
| Attachments > 25MB                    | Skipped. Warning logged with filename and source URL.                |

---

## Per-Platform API Specifics

### Rate Limits & Concurrency Strategy

| Platform    | Limit              | Strategy                                                      |
|-------------|-------------------|---------------------------------------------------------------|
| Notion      | 3 req/sec          | Strict token bucket, single-threaded extractor               |
| Airtable    | 5 req/sec per base | Per-base token bucket; bases can run in parallel             |
| Wrike       | ~100–150 req/min   | Conservative token bucket; folder ops serialized             |
| Smartsheet  | ~300 req/min       | Bulk row inserts up to 500 rows/request                      |
| Asana       | 1,500 req/min      | High concurrency; max 50 concurrent GETs                     |
| Trello      | 300 req/10s        | Token bucket; key limit vs token limit tracked separately    |
| Jira        | 100 req/sec GET    | High concurrency; 50 issues per bulk create                  |
| Monday.com  | 5M complexity/query| Per-query complexity tracking; avoid over-fetching           |

### Platform-Specific Implementation Notes

**Asana:**
- `download_url` on attachments expires in ~2 minutes — download immediately in same goroutine after task fetch
- Tasks can belong to multiple projects — pick canonical parent, log others
- Subtasks are recursive Task objects — walk tree, map to parent/child rows
- Sections map to parent rows or row grouping
- Tags → `MULTI_PICKLIST` column

**Monday.com:**
- GraphQL only — use `github.com/hasura/go-graphql-client`
- HTTP 200 returned even on errors — always check `errors[]` in response
- `color` (Status) columns use numeric label IDs — resolve to display string via board metadata
- `name` column is always first and cannot be deleted
- 10,000 items/board hard limit — shard large boards into multiple sheets
- Subitems live on a hidden sub-board with separate column schema
- Update (comment) bodies are HTML — strip before storing

**Trello:**
- Comments are Actions of type `commentCard` — retrieve via `/cards/{id}/actions?filter=commentCard`
- Custom Fields are a Power-Up — check if enabled per board before querying
- Lists are ordering containers — map to a `PICKLIST` column value (list name = option)
- Checklists → child rows or a dedicated checklist sheet

**Jira:**
- v3 API uses ADF for `description` and `comment.body` — walk ADF node tree to extract plain text
- `customfield_NNNNN` IDs are instance-specific — dynamically resolve field metadata via `/rest/api/3/field`
- Epic hierarchy: next-gen uses `parent` field; classic uses `customfield_10014` (Epic Link) — handle both
- Sprints in `customfield_10020` — extract active sprint name
- Worklogs → `Duration` canonical type → `TEXT_NUMBER` formatted as "Xh Ym"
- Attachments require auth — download with API token during migration

**Airtable:**
- 5 req/sec per base; only 10 records per batch — large bases require many calls
- Attachment URLs expire in 2 hours — download immediately
- To create attachments in Smartsheet: download binary, re-upload via multipart
- Linked record fields store record IDs — resolve to display values for Smartsheet cells
- Formula/rollup/lookup/count fields are read-only — snapshot value only

**Notion:**
- Rate limit is very low (3 req/sec) — requires heavy rate-limit management
- `title` property is mandatory in every database — becomes primary name column
- Page body = Blocks (separate API call per page) — optionally migrate as text snapshot
- File URLs expire after 1 hour — download immediately
- `status` property cannot be created via API — use `PICKLIST` instead
- Relation properties require both databases accessible to the integration
- Synced blocks — dereference before migrating

**Wrike:**
- Projects are Folders with extra metadata — same `/folders` endpoint, check `project` field
- Tasks can have multiple `parentIds` — pick canonical parent
- `description` is HTML — strip tags
- Custom field values reference account-level field definitions by GUID — resolve via `/customfields`
- Workflow `customStatusId` → resolve to status label via `/workflows`
- Folder operations must be sequential (no parallel folder create/update)
- Timelogs → `Duration` canonical type

---

## CLI Design

### Interactive Flow

```
$ migrate-to-smartsheet

? Select source platform:             (arrow keys)
? [Source] API key:                   (masked input)
  Connecting... ✓

? Select workspace(s) to migrate:     (multi-select checkboxes)
? Select projects to migrate:         (multi-select; "all" option)

? Filter by date range? (optional):   (no filter / created-after / updated-after)
? Load user mapping file? (optional): (path to CSV: source_id,smartsheet_email)

? Smartsheet API key:                 (masked input)
  Connecting to Smartsheet... ✓

? Target Smartsheet workspace:        (create new / select existing)
? Sheet naming convention:            (as-is / prefix / suffix / template)
? If sheet already exists:            (skip & warn / rename with timestamp / overwrite)
? Fields to exclude: (optional):      (multi-select or freeform field names)

? Migration options:
    [x] Migrate attachments (download & re-upload)
    [x] Migrate comments
    [x] Migrate subtasks as child rows
    [ ] Include archived/completed items
    [x] Resume if interrupted

  Analyzing source data...

╔══════════════════════════╗
║    Migration Preview     ║
╠══════════════════════════╣
║ Workspaces:    1         ║
║ Sheets:        23        ║
║ Rows:          4,821     ║
║ Columns:       147       ║
║ Attachments:   312       ║
║ Comments:      2,103     ║
║ Warnings:      3         ║
╚══════════════════════════╝

  Warnings:
  ⚠  8 formula fields → static value snapshots
  ⚠  14 tasks in multiple projects → first project used
  ⚠  2 attachments exceed 25MB → will be skipped

? Proceed with migration? (y/N):
```

### Non-Interactive Flags

```bash
migrate-to-smartsheet \
  --source          asana|monday|trello|jira|airtable|notion|wrike \
  --source-token    $SOURCE_TOKEN \
  --smartsheet-token $SS_TOKEN \
  --workspace       "Workspace Name" \
  --projects        "Project A,Project B"  \  # comma-separated; omit = all
  --created-after   2023-01-01 \              # ISO 8601 date
  --updated-after   2023-06-01 \
  --user-map        ./users.csv \             # CSV: source_id,smartsheet_email
  --sheet-prefix    "Asana: " \
  --sheet-suffix    "" \
  --sheet-name-template "{project}" \         # {source}, {project}, {date}
  --conflict        skip|rename|overwrite \
  --exclude-fields  "formula,created_at" \
  --include-attachments \
  --include-comments \
  --include-archived \
  --yes                                       # skip confirmation prompt
```

### Resumability

On interruption, re-running detects `.migrate-state-YYYY-MM-DD.json` and prompts to resume:

```
? Found incomplete migration from 2026-03-01 (78% complete). Resume? (Y/n)
✔ Skipping 18 already-completed sheets
↻ Resuming from sheet 19/23...
```

State file format:
```json
{
  "source": "asana",
  "started_at": "2026-03-01T14:30:00Z",
  "completed_sheets": ["sheet_id_1", "sheet_id_2"],
  "partial_sheet": {
    "source_id": "project_abc",
    "smartsheet_id": 123456789,
    "last_completed_row": 2100
  }
}
```

---

## Key Go Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/AlecAivazis/survey/v2` | Interactive prompts |
| `github.com/schollz/progressbar/v3` | Progress bars |
| `github.com/hasura/go-graphql-client` | Monday.com GraphQL |
| `golang.org/x/net/html` | HTML tag stripping (Wrike, Monday) |
| `golang.org/x/time/rate` | Token bucket rate limiter |
| `github.com/stretchr/testify` | Test assertions |

---

## Distribution

**GitHub Releases via GoReleaser** on tag push:
```
migrate-to-smartsheet_darwin_amd64.tar.gz
migrate-to-smartsheet_darwin_arm64.tar.gz
migrate-to-smartsheet_linux_amd64.tar.gz
migrate-to-smartsheet_linux_arm64.tar.gz
migrate-to-smartsheet_windows_amd64.zip
checksums.txt
sbom.json
```

Optional: Homebrew tap for `brew install yourname/tap/migrate-to-smartsheet`

---

## Testing Strategy

- Each extractor unit-tested against recorded fixture JSON in `testdata/` — no live API calls in CI
- Transformer unit tests for every field type mapping and edge case:
  - HTML strip, ADF parse, multi-parent resolution, user remapping, date formatting
- `--test-mode` flag for integration testing against real APIs with small sample datasets
- GitHub Actions CI: test + lint on every PR; GoReleaser release on tag push

---

## Explicit Non-Goals (v1.0)

- No migration of Reports or Dashboards (Smartsheet API does not support creating these programmatically)
- No bidirectional sync
- No scheduled/recurring migration
- No web UI
- No deletion of source data (non-destructive by design)
