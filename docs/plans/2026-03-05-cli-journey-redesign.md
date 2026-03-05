# CLI Journey Redesign

**Date:** 2026-03-05
**Status:** Approved
**Scope:** Replace migration preview box, add per-project live status, post-migration interactive menu with log viewer, GitHub error reporting, workspace browser opener, and failed-sheet re-run.

---

## Problem

The current CLI journey has three weak points:

1. **Preview is meaningless** — a box showing Sheets / Rows / Columns / Attachments gives the user no way to verify what is actually being migrated where. They cannot see project names, destination sheet names, or what might go wrong.

2. **Migration runs silently** — a single progress bar advancing from 0→N gives no per-project feedback. Errors only surface at the end.

3. **Tool exits immediately on completion** — there is nothing to do after. No way to open the result, review what happened, report failures, or retry.

---

## Design

### Phase 2 — Mapping Preview (replaces the `╔══╗` box)

After projects are extracted and before asking the user to confirm, render the exact source → destination structure:

```
  Scanning your Asana workspace...  ✓

  📦  Migration Map
  ─────────────────────────────────────────────────────────────────
  📁  My workspace  →  Smartsheet: My workspace

      🅰️  Campaign management by ClassPass
          → Sheet "Campaign management by ClassPass"
             42 tasks · 3 attachments · contact columns

      🅰️  Product Launch Q2
          → Sheet "Product Launch Q2"
             7 tasks

      🅰️  Engineering Backlog
          → Sheet "Engineering Backlog"
             12 tasks · assignees

      ⚠️  P.U.C.K's first project
          → Sheet "P.U.C.K's first project"
             3 tasks · ⚠ 1 attachment >25MB will be skipped
  ─────────────────────────────────────────────────────────────────
  4 sheets · 64 tasks · 2 attachments transferable · 1 warning

? Proceed with migration?  ›  Yes / No
```

**Rules:**
- Every project shows its exact destination sheet name
- Item count uses domain language ("tasks", "cards", "issues", "records") not "rows"
- Notable features called out inline: attachments, contact columns, comments
- Warnings appear on the specific project they affect, not as a global count
- Summary line at the bottom: sheets · items · transferable attachments · warnings

---

### Phase 3 — Live Migration Progress

Replace the single overall bar with per-project status lines that update in place, plus an overall bar pinned to the bottom:

```
  ─────────────────────────────────────────────────────────────────
  ✓  Campaign management by ClassPass    42 items · 3 attachments
  ✓  Product Launch Q2                    7 items
  ⟳  Engineering Backlog...
  ·  P.U.C.K's first project

  ████████████████████░░░░░░░░░░  3 / 4
```

**Status icons:**
- `·`  pending (not yet started)
- `⟳`  currently migrating
- `✓`  completed successfully
- `✗`  failed

**Failure behaviour:** errors appear inline on the project line immediately — do not wait until the end:
```
  ✗  P.U.C.K's first project   row insert failed (errorCode 1012)
```

---

### Phase 4 — Completion Summary

Print the final per-project table, a one-line summary, and transition to the post-migration menu:

```
  ─────────────────────────────────────────────────────────────────
  ✓  Campaign management by ClassPass    42 items
  ✓  Product Launch Q2                    7 items
  ✓  Engineering Backlog                 12 items
  ✗  P.U.C.K's first project             row insert failed (errorCode 1012)

  Migration complete  ·  3 of 4 sheets  ·  61 items
  ─────────────────────────────────────────────────────────────────
```

---

### Phase 5 — Post-Migration Menu (new)

Instead of exiting, present an interactive menu. Actions are context-aware (e.g. "Report errors" only shown if there were failures, "Re-run failed" only shown if there were failures):

```
? What would you like to do next?
  › 🚀  Open Smartsheet workspace in browser
    📋  View migration log
    🐛  Report errors to GitHub     ← only shown if errors exist
    🔄  Re-run failed sheets        ← only shown if errors exist
    👋  Exit
```

#### 🚀 Open Smartsheet workspace in browser

If a single workspace was created → open it directly.

If multiple workspaces were created → show a sub-menu:

```
? Which workspace would you like to open?
  › 📁  My workspace        (4 sheets)
    📁  Engineering          (2 sheets)
    ← Back
```

Each workspace entry uses the `permalink` URL returned by the Smartsheet `CreateWorkspace` API and stored in the migration result. Opens using `open` (macOS), `xdg-open` (Linux), or `start` (Windows).

#### 📋 View migration log

Renders the NDJSON log file as a human-readable summary grouped by project:

```
  Migration log — asana — 2026-03-05 17:17

  ✓  Campaign management by ClassPass
     42 rows migrated · 3 attachments uploaded · 0 errors

  ✓  Product Launch Q2
     7 rows migrated · 0 attachments · 0 errors

  ✗  P.U.C.K's first project
     0 rows migrated
     Error: row insert failed — smartsheet errorCode 1012 "Unable to parse request"

  Summary: 3 succeeded · 1 failed · 61 items total
```

Not the raw JSON — parsed and formatted. Errors shown in red, warnings in yellow.

#### 🐛 Report errors to GitHub

Only shown when the migration had at least one error or warning.

**Anonymisation rules — strip / replace:**

| Data | Replaced with |
|---|---|
| Project / sheet names | `[project_N]` |
| Cell values | `[redacted]` |
| Email addresses | `[email]` |
| API tokens | `[token]` |
| Non-API URLs | `[url]` |
| User names / display names | `[user]` |

**Keep as-is (safe, diagnostic):**
- Error codes (`1012`, `1235`, `4000`)
- Smartsheet API error message text
- Platform name (`asana`, `jira`, etc.)
- Column types (`TEXT_NUMBER`, `PICKLIST`, `CONTACT_LIST`, etc.)
- Row and column counts
- Go stack traces / panic output

**Output:**
Constructs a GitHub new-issue URL with title and body pre-filled:
```
https://github.com/glitchedgod/migrate-to-smartsheet/issues/new
  ?title=Migration+error%3A+asana+%E2%86%92+errorCode+1012
  &body=...anonymised+report...
  &labels=bug
```
Opens this URL in the browser. User reviews the pre-filled content and clicks Submit. No GitHub token required.

Print a confirmation line after opening:
```
  🌐 Opened GitHub issue form in your browser
     github.com/glitchedgod/migrate-to-smartsheet/issues/new
```

#### 🔄 Re-run failed sheets

Only shown when the migration had at least one failure.

Clears the failed project IDs from the state file (keeps the succeeded ones), then re-enters the migration loop for only the failed projects. Effectively a targeted resume.

On completion, returns to the post-migration menu.

#### 👋 Exit

Clean exit, code 0 if all succeeded, code 1 if any failures remained after the final attempt.

---

## Implementation Plan

### New package: `internal/postmigration/`

**`menu.go`** — post-migration interactive menu driver
- `MigrationResult` struct: holds per-project outcomes, workspace permalinks, log file path
- `RunMenu(result MigrationResult)` — main entry point called from `run.go` after migration
- Loops menu until user selects Exit or Re-run completes cleanly

**`logview.go`** — human-readable log renderer
- `PrintLogSummary(logPath string)` — parses NDJSON, groups by project, renders with colour

**`report.go`** — anonymiser + GitHub URL builder
- `Anonymise(raw string) string` — applies regex replacements for all PII patterns
- `BuildGitHubIssueURL(platform string, errors []ErrorEntry) string` — constructs the pre-filled URL
- `OpenBrowser(url string) error` — cross-platform `open`/`xdg-open`/`start`

### Modified: `cmd/migrate/run.go`

1. **Preview box → mapping preview** — replace `fmt.Printf(╔...╗)` with `printMappingPreview()`
2. **Progress bar → per-project status** — replace `progressbar.Default` with a custom `StatusTracker` that maintains per-project lines and the overall bar
3. **Completion** — build `MigrationResult` from the loop outcomes, call `postmigration.RunMenu(result)`
4. **Workspace permalink** — store permalink from `CreateWorkspace` response in `MigrationResult`

### Modified: `internal/loader/smartsheet/loader.go`

- `CreateWorkspace` returns `(id int64, permalink string, err error)` — add `permalink` to the response struct

### Modified: `internal/miglog/miglog.go`

- Add `ListErrors() []ErrorEntry` method — reads back the NDJSON file and returns entries at ERROR level

---

## Domain Language Map

To use the right noun in the preview per platform:

| Platform | Item noun |
|---|---|
| Asana | tasks |
| Monday.com | items |
| Trello | cards |
| Jira | issues |
| Airtable | records |
| Notion | pages |
| Wrike | tasks |

---

## Out of Scope

- Sending the GitHub issue via API (requires user's GitHub token)
- Diff view showing what changed vs what was already in Smartsheet
- Email notifications on completion
