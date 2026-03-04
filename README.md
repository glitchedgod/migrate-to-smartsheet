# migrate-to-smartsheet

Migrate data from project management tools into Smartsheet. Single binary. Non-destructive. Resumable.

## Supported Sources

| Platform | Auth |
|----------|------|
| Asana | Personal Access Token |
| Monday.com | API Token |
| Trello | API Key + OAuth Token (see below) |
| Jira Cloud | Email + API Token |
| Airtable | Personal Access Token |
| Notion | Integration Token |
| Wrike | Permanent Token |

### Getting Credentials

**Asana** — https://app.asana.com/0/my-profile-settings/apps → Personal access tokens → Create new token

**Monday.com** — Avatar → Administration → API → copy your personal API token

**Trello** — Trello no longer exposes tokens directly. You need to create a Power-Up first:
1. Go to https://trello.com/power-ups/admin → **New** → fill in any name and workspace → Create
2. Click **API Key** in the sidebar — copy your key
3. Generate a token by visiting in your browser (replace `YOUR_KEY`):
   ```
   https://trello.com/1/authorize?expiration=never&name=migrate-to-smartsheet&scope=read&response_type=token&key=YOUR_KEY
   ```
4. Click Allow → copy the token
5. Use `--source-key YOUR_KEY --source-token YOUR_TOKEN`

**Jira Cloud** — https://id.atlassian.com/manage-profile/security/api-tokens → Create API token
Use `--jira-email your@email.com --jira-instance https://yourorg.atlassian.net --source-token YOUR_TOKEN`

**Airtable** — https://airtable.com/create/tokens → Create token with `data.records:read` and `schema.bases:read` scopes

**Notion** — https://www.notion.so/my-integrations → New integration → copy the secret.
Then for each database: open it in Notion → `...` menu → Add connections → select your integration.

**Wrike** — https://www.wrike.com/frontend/apps/index.html#api → Permanent tokens → Create new

**Smartsheet (destination)** — Avatar → Personal Settings → API access → Generate new access token

## Installation

**Direct download:** Download from [Releases](https://github.com/glitchedgod/migrate-to-smartsheet/releases)

**Go install:**
```bash
go install github.com/glitchedgod/migrate-to-smartsheet/cmd/migrate@latest
```

## Usage

### Interactive

```bash
migrate-to-smartsheet
```

Prompts for platform, credentials, workspace selection, options, and shows a preview before migrating.

### Non-interactive

```bash
migrate-to-smartsheet \
  --source asana \
  --source-token $ASANA_TOKEN \
  --smartsheet-token $SS_TOKEN \
  --projects "Project A,Project B" \
  --include-attachments \
  --include-comments \
  --yes
```

### Full flag reference

```
--source              Source platform (asana|monday|trello|jira|airtable|notion|wrike)
--source-token        Source API token
--smartsheet-token    Smartsheet API token
--workspace           Source workspace name (interactive prompt if omitted)
--projects            Comma-separated project names (all if omitted)
--created-after       ISO 8601 date — only items created after this date
--updated-after       ISO 8601 date — only items updated after this date
--user-map            CSV file: source_id,smartsheet_email
--sheet-prefix        Prefix added to each sheet name
--sheet-suffix        Suffix added to each sheet name
--sheet-name-template Sheet naming template: {source}, {project}, {date}
--conflict            Conflict handling: skip (default)|rename|overwrite
--exclude-fields      Comma-separated fields to exclude
--include-attachments Download and re-upload attachments (default: true)
--include-comments    Migrate comments (default: true)
--include-archived    Include archived/completed items (default: false)
--yes                 Skip confirmation prompt
--jira-email          Jira account email (Jira only)
--jira-instance       Jira Cloud URL, e.g. https://yourorg.atlassian.net
--source-key          Trello API key (Trello only)
```

## Resumability

If a migration is interrupted, re-running automatically detects the state file and prompts to resume from where it left off.

## What Gets Migrated

- Tasks / cards / issues / records → Smartsheet rows
- Custom fields → Smartsheet columns (type-mapped)
- Attachments → downloaded and re-uploaded (≤25MB)
- Comments → row discussions
- Subtasks → child rows
- User assignments → contact columns (with optional user remapping)

## What Does NOT Get Migrated (v1.0)

- Reports and dashboards
- Formula/computed field logic (values are snapshotted)
- Items in multiple projects (canonical parent only)
