# migrate-to-smartsheet

Migrate data from project management tools into Smartsheet. Single binary. Non-destructive. Resumable.

[![CI](https://github.com/glitchedgod/migrate-to-smartsheet/actions/workflows/ci.yml/badge.svg)](https://github.com/glitchedgod/migrate-to-smartsheet/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/glitchedgod/migrate-to-smartsheet)](https://github.com/glitchedgod/migrate-to-smartsheet/releases/latest)

## Supported Sources

| Platform | What migrates |
|---|---|
| **Asana** | Projects → sheets, tasks → rows, assignees, due dates, attachments |
| **Monday.com** | Boards → sheets, items → rows, status/date/people columns |
| **Trello** | Boards → sheets, cards → rows, lists as dropdown, labels, due dates |
| **Jira Cloud** | Projects → sheets, issues → rows, status/priority/type as dropdowns, assignees |
| **Airtable** | Tables → sheets, records → rows, all field types including selects and contacts |
| **Notion** | Databases → sheets, pages → rows, typed properties |
| **Wrike** | Projects → sheets, tasks → rows, status, due dates, descriptions |

---

## Installation

**Download a pre-built binary** (recommended):

**macOS (Apple Silicon)**
```bash
curl -L https://github.com/glitchedgod/migrate-to-smartsheet/releases/latest/download/migrate-to-smartsheet_0.3.1_darwin_arm64.tar.gz | tar -xz
chmod +x migrate-to-smartsheet
./migrate-to-smartsheet
```

**macOS (Intel)**
```bash
curl -L https://github.com/glitchedgod/migrate-to-smartsheet/releases/latest/download/migrate-to-smartsheet_0.3.1_darwin_amd64.tar.gz | tar -xz
chmod +x migrate-to-smartsheet
./migrate-to-smartsheet
```

**Linux (amd64)**
```bash
curl -L https://github.com/glitchedgod/migrate-to-smartsheet/releases/latest/download/migrate-to-smartsheet_0.3.1_linux_amd64.tar.gz | tar -xz
chmod +x migrate-to-smartsheet
./migrate-to-smartsheet
```

**Linux (arm64)**
```bash
curl -L https://github.com/glitchedgod/migrate-to-smartsheet/releases/latest/download/migrate-to-smartsheet_0.3.1_linux_arm64.tar.gz | tar -xz
chmod +x migrate-to-smartsheet
./migrate-to-smartsheet
```

**Windows (PowerShell)**
```powershell
Invoke-WebRequest -Uri https://github.com/glitchedgod/migrate-to-smartsheet/releases/latest/download/migrate-to-smartsheet_0.3.1_windows_amd64.zip -OutFile migrate.zip
Expand-Archive migrate.zip .
.\migrate-to-smartsheet.exe
```

**Build from source** (requires Go 1.22+):
```bash
git clone https://github.com/glitchedgod/migrate-to-smartsheet
cd migrate-to-smartsheet
go build -o migrate-to-smartsheet ./cmd/migrate/
./migrate-to-smartsheet
```

---

## Quick Start

### Interactive mode (recommended for first use)

Just run the binary with no flags:

```bash
./migrate-to-smartsheet
```

The tool will:
1. Ask you to select your source platform
2. Show you a **step-by-step guide** for getting your API credentials for that platform
3. Walk you through Smartsheet token setup
4. Let you pick which projects/boards to migrate
5. Show a **preview** (sheet count, row count, warnings) before touching anything
6. Ask for confirmation, then migrate

### Non-interactive / scripted mode

Pass all credentials via flags and use `--yes` to skip prompts:

```bash
./migrate-to-smartsheet \
  --source asana \
  --source-token "$ASANA_TOKEN" \
  --smartsheet-token "$SS_TOKEN" \
  --yes
```

---

## Platform Examples

**Asana**
```bash
./migrate-to-smartsheet \
  --source asana \
  --source-token "1/xxxxxxxxxxxxx:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

**Monday.com**
```bash
./migrate-to-smartsheet \
  --source monday \
  --source-token "eyJhbGciOiJIUzI1NiJ9..." \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

**Trello** (requires both API key and OAuth token)
```bash
./migrate-to-smartsheet \
  --source trello \
  --source-key "your-api-key" \
  --source-token "your-oauth-token" \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

**Jira Cloud** (requires instance URL and email)
```bash
./migrate-to-smartsheet \
  --source jira \
  --source-token "your-api-token" \
  --jira-email "you@yourorg.com" \
  --jira-instance "https://yourorg.atlassian.net" \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

**Airtable**
```bash
./migrate-to-smartsheet \
  --source airtable \
  --source-token "patXXXXXXXXXXXXXX.xxxxxxxx" \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

**Notion**
```bash
./migrate-to-smartsheet \
  --source notion \
  --source-token "ntn_xxxxxxxxxxxxx" \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

**Wrike**
```bash
./migrate-to-smartsheet \
  --source wrike \
  --source-token "your-permanent-token" \
  --smartsheet-token "your-smartsheet-token" \
  --yes
```

---

## Getting API Credentials

The interactive mode walks you through this automatically. If you prefer to set things up manually:

**Asana** — [My Settings](https://app.asana.com/0/my-tasks/list) → Apps → Personal Access Tokens → Create new token

**Monday.com** — Avatar (top-right) → Administration → Connections → API → Copy personal API token (v2)

**Trello** — Two steps:
1. [Power-Ups Admin](https://trello.com/power-ups/admin) → New → get your API Key
2. Open this URL (replace `YOUR_KEY`): `https://trello.com/1/authorize?expiration=never&name=migrate-to-smartsheet&scope=read&response_type=token&key=YOUR_KEY` → Allow → copy the token

**Jira Cloud** — [API Tokens](https://id.atlassian.com/manage-profile/security/api-tokens) → Create API token

**Airtable** — [Create token](https://airtable.com/create/tokens) → add scopes `data.records:read` + `schema.bases:read`

**Notion** — [My integrations](https://www.notion.so/my-integrations) → New integration → copy the secret. Then for each database: Share → Invite your integration.

**Wrike** — Profile → Apps & Integrations → API → Create new app → Generate permanent token

**Smartsheet** — Account (top-right) → Personal Settings → API Access → Generate new access token

---

## All Flags

```
--source                Source platform: asana|monday|trello|jira|airtable|notion|wrike
--source-token          Source platform API token
--source-key            Trello API key (Trello only)
--smartsheet-token      Smartsheet API token
--jira-email            Jira account email (Jira only)
--jira-instance         Jira Cloud URL, e.g. https://yourorg.atlassian.net (Jira only)
--workspace             Source workspace name (prompted interactively if omitted)
--projects              Comma-separated project/board names to migrate (default: all)
--created-after         Only migrate items created after this date (ISO 8601, e.g. 2025-01-01)
--updated-after         Only migrate items updated after this date (ISO 8601)
--user-map              Path to CSV file mapping source user IDs to Smartsheet emails
--sheet-prefix          Prefix added to each created sheet name
--sheet-suffix          Suffix added to each created sheet name
--sheet-name-template   Sheet naming template using {source}, {project}, {date}
--conflict              How to handle existing sheets: skip (default) | rename | overwrite
--exclude-fields        Comma-separated field names to exclude from migration
--include-attachments   Download and re-upload file attachments (default: true)
--include-comments      Migrate comments as row discussions (default: true)
--include-archived      Include completed/archived items (default: false)
--yes                   Skip all confirmation prompts (for CI/scripting)
```

---

## Output

Each migration creates:

- **Smartsheet sheets** — one per project/board, named after the source project
- **A log file** — `.migrate-log-{source}-{timestamp}.log` with a full structured record of what happened

```
✓ Migration complete!
  📋 Log: .migrate-log-asana-2026-03-05-11-06-26.log
```

The log is NDJSON (one JSON object per line) — inspect it with:
```bash
cat .migrate-log-asana-*.log | jq .
```

---

## Resuming an Interrupted Migration

If a migration stops mid-way (network error, rate limit, etc.), re-run the same command. The tool detects the state file and asks:

```
Found incomplete migration from 2026-03-05 11:06 (2 sheets done). Resume? (Y/n)
```

---

## What Gets Migrated

| Source concept | Smartsheet equivalent |
|---|---|
| Project / Board / Database | Sheet |
| Task / Card / Issue / Record | Row |
| Text fields | TEXT_NUMBER column |
| Date fields | DATE column |
| Single-select / Status | PICKLIST column (options auto-populated from source values) |
| Multi-select | MULTI_PICKLIST column |
| Checkbox / Boolean | CHECKBOX column |
| Assignee / Contact | CONTACT_LIST column |
| Attachments (≤25 MB) | Row attachments (downloaded and re-uploaded) |
| Comments | Row discussions |
| Subtasks | Child rows (indented under parent) |

## What Is Not Migrated

- Reports and dashboards
- Formula / computed field logic (values are snapshotted as plain text)
- Items belonging to multiple projects (canonical parent only)
- Attachments larger than 25 MB (skipped with a warning)
