# migrate-to-smartsheet

Migrate data from project management tools into Smartsheet. Single binary. Non-destructive. Resumable.

## Supported Sources

| Platform | Auth |
|----------|------|
| Asana | Personal Access Token |
| Monday.com | API Token |
| Trello | API Key + Token |
| Jira Cloud | Email + API Token |
| Airtable | Personal Access Token |
| Notion | Integration Token |
| Wrike | Permanent Token |

## Installation

**Direct download:** Download from [Releases](https://github.com/bchauhan/migrate-to-smartsheet/releases)

**Go install:**
```bash
go install github.com/bchauhan/migrate-to-smartsheet/cmd/migrate@latest
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
