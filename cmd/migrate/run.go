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
	airtableext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/airtable"
	asanaext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/asana"
	jiraext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/jira"
	mondayext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/monday"
	notionext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/notion"
	trelloext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/trello"
	wrikeext "github.com/glitchedgod/migrate-to-smartsheet/internal/extractor/wrike"
	ssloader "github.com/glitchedgod/migrate-to-smartsheet/internal/loader/smartsheet"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/miglog"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/preview"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/state"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/wizard"
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

	// Splash screen — shown once in interactive mode
	if !yes {
		printSplash()
	}

	// Interactive prompts for missing required values — skip when --yes or non-TTY
	if sourceStr == "" && !yes {
		// Platform options with icons for a delightful experience
		platformOptions := []string{
			"🅰️   asana     — Asana",
			"📅  monday    — Monday.com",
			"📋  trello    — Trello",
			"🔷  jira      — Jira Cloud",
			"🗂️   airtable  — Airtable",
			"📓  notion    — Notion",
			"✍️   wrike     — Wrike",
		}
		var platformChoice string
		survey.AskOne(&survey.Select{ //nolint:errcheck
			Message: "Which platform are you migrating from?",
			Options: platformOptions,
		}, &platformChoice)
		// Extract the platform key from the chosen option
		for _, key := range []string{"asana", "monday", "trello", "jira", "airtable", "notion", "wrike"} {
			if strings.Contains(platformChoice, key) {
				sourceStr = key
				break
			}
		}
	}
	if sourceToken == "" && !yes {
		creds, err := wizard.ShowAndPrompt(sourceStr)
		if err != nil {
			return fmt.Errorf("credential wizard: %w", err)
		}
		sourceToken = creds.Token
		// Trello: wizard collects API key separately
		if creds.Key != "" {
			_ = cmd.Flags().Set("source-key", creds.Key)
		}
		// Jira: wizard collects instance URL and email
		if creds.Instance != "" {
			_ = cmd.Flags().Set("jira-instance", creds.Instance)
		}
		if creds.Email != "" {
			_ = cmd.Flags().Set("jira-email", creds.Email)
		}
	}
	if sourceStr == "" {
		return fmt.Errorf("--source is required")
	}
	if sourceToken == "" {
		return fmt.Errorf("--source-token is required")
	}

	ext, err := buildExtractor(sourceStr, sourceToken, cmd)
	if err != nil {
		return fmt.Errorf("building extractor: %w", err)
	}

	// Structured log file — always created, even with --yes.
	logFile := fmt.Sprintf(".migrate-log-%s-%s.log", sourceStr, time.Now().Format("2006-01-02-15-04-05"))
	mlog, logErr := miglog.New(logFile, sourceStr)
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "  ⚠  Could not create log file: %v\n", logErr)
		mlog = nil
	} else {
		defer func() { _ = mlog.Close() }()
		mlog.Info("migration started", "source", sourceStr)
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
			wsNames[i] = "📁  " + ws.Name
		}
		var wsChoice string
		survey.AskOne(&survey.Select{Message: "Select workspace:", Options: wsNames}, &wsChoice) //nolint:errcheck
		for i, ws := range workspaces {
			if wsNames[i] == wsChoice {
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
		projIcon := platformProjectIcon(sourceStr)
		projNames := make([]string, len(projectRefs))
		for i, p := range projectRefs {
			projNames[i] = projIcon + "  " + p.Name
		}
		var chosen []string
		survey.AskOne(&survey.MultiSelect{ //nolint:errcheck
			Message: "Select projects to migrate (space to select, enter to confirm):",
			Options: projNames,
		}, &chosen)
		chosenSet := make(map[string]bool, len(chosen))
		for _, c := range chosen {
			chosenSet[c] = true
		}
		for i, p := range projectRefs {
			if chosenSet[projNames[i]] {
				selectedProjects = append(selectedProjects, p)
			}
		}
	}

	if len(selectedProjects) == 0 {
		fmt.Println("No projects selected. Exiting.")
		return nil
	}

	// Smartsheet setup
	if ssToken == "" && !yes {
		creds, err := wizard.ShowAndPrompt("smartsheet")
		if err != nil {
			return fmt.Errorf("smartsheet credential wizard: %w", err)
		}
		ssToken = creds.Token
	}
	if ssToken == "" {
		return fmt.Errorf("--smartsheet-token is required")
	}
	loader := ssloader.New(ssToken)

	// Create (or reuse) a Smartsheet workspace mirroring the source workspace.
	// Platforms where the "workspace" is a synthetic placeholder (Jira = instance URL,
	// Wrike = account ID) get a cleaned-up name; the others get the real workspace name.
	ssWorkspaceID := int64(0)
	ssWorkspaceName := selectedWorkspace.Name
	// For Jira, use a shorter display name derived from the instance URL
	if sourceStr == "jira" {
		ssWorkspaceName = strings.TrimPrefix(strings.TrimPrefix(selectedWorkspace.Name, "https://"), "http://")
	}
	// For Airtable, clarify that this is a Base (matches user mental model)
	if sourceStr == "airtable" {
		ssWorkspaceName = selectedWorkspace.Name + " (Airtable)"
	}
	if ssWorkspaceName != "" {
		wsID, wsErr := loader.CreateWorkspace(ctx, ssWorkspaceName)
		if wsErr != nil {
			fmt.Fprintf(os.Stderr, "  ⚠  Could not create Smartsheet workspace %q: %v — sheets will be created at root\n", ssWorkspaceName, wsErr)
		} else {
			ssWorkspaceID = wsID
			fmt.Printf("  📁 Smartsheet workspace: %s\n", ssWorkspaceName)
			if mlog != nil {
				mlog.Info("smartsheet workspace created", "workspace", ssWorkspaceName, "workspace_id", wsID)
			}
		}
	}

	// Resume state
	stateFile := fmt.Sprintf(".migrate-state-%s-%d.json", sourceStr, time.Now().Unix())
	var migState *state.MigrationState
	if entries, err := os.ReadDir("."); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".migrate-state-"+sourceStr+"-") && strings.HasSuffix(name, ".json") {
				if existing, err := state.Load(name); err == nil {
					var resume bool
					survey.AskOne(&survey.Confirm{ //nolint:errcheck
						Message: fmt.Sprintf("Found incomplete migration from %s (%d sheets done). Resume?",
							existing.StartedAt.Format("2006-01-02 15:04"), len(existing.CompletedSheets)),
						Default: true,
					}, &resume)
					if resume {
						migState = existing
						stateFile = name
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
	userMap := transformer.NewUserMap()
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
		if len(excludeFields) > 0 {
			extracted = applyExcludeFields(extracted, excludeFields)
		}
		allProjects = append(allProjects, *extracted)
	}

	// Preview
	previewWorkspaces := []model.Workspace{{ID: selectedWorkspace.ID, Name: selectedWorkspace.Name, Projects: allProjects}}
	summary := preview.Summarize(previewWorkspaces)
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
	errCount := 0
	warnCount := len(summary.Warnings)
	for i := range allProjects {
		proj := &allProjects[i]

		// Apply value transforms to all cells
		colTypeByName := make(map[string]model.ColumnType, len(proj.Columns))
		for _, col := range proj.Columns {
			colTypeByName[col.Name] = col.Type
		}
		for ri := range proj.Rows {
			for ci := range proj.Rows[ri].Cells {
				cell := &proj.Rows[ri].Cells[ci]
				colType := colTypeByName[cell.ColumnName]
				cell.Value = transformer.TransformCellValue(cell.Value, colType, userMap)
			}
		}

		// Apply sheet naming
		sheetName := applySheetNaming(proj.Name, sourceStr, sheetPrefix, sheetSuffix, sheetTemplate)
		proj.Name = sheetName

		// Deduplicate column names before creating the sheet.
		// Smartsheet returns HTTP 500 when two columns share the same name.
		deduplicateProjectColumns(proj)

		// Handle conflict check (skip is default — if sheet exists, we'll get an API error and log it)
		_ = conflictMode // TODO: implement rename/overwrite in loader

		if mlog != nil {
			mlog.Info("migrating sheet",
				"sheet", sheetName,
				"rows", len(proj.Rows),
				"columns", len(proj.Columns),
				"smartsheet_workspace", ssWorkspaceName,
			)
		}

		sheetID, colMap, contactColIDs, err := loader.CreateSheet(ctx, proj, ssWorkspaceID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to create sheet %s: %v\n", sheetName, err)
			if mlog != nil {
				mlog.SheetFailed(sheetName, err)
			}
			allSucceeded = false
			errCount++
			_ = bar.Add(1)
			continue
		}

		if mlog != nil {
			mlog.Info("sheet created",
				"sheet", sheetName,
				"sheet_id", sheetID,
				"smartsheet_workspace_id", ssWorkspaceID,
			)
		}

		rowIDMap, err := loader.BulkInsertRows(ctx, sheetID, proj.Rows, colMap, contactColIDs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to insert rows for %s: %v\n", sheetName, err)
			if mlog != nil {
				mlog.Error("row insert failed",
					"sheet", sheetName,
					"sheet_id", sheetID,
					"error", err.Error(),
				)
			}
			allSucceeded = false
			errCount++
		}

		// Attachments
		attachCount := 0
		if includeAttachments {
			for _, row := range proj.Rows {
				ssRowID, ok := rowIDMap[row.ID]
				if !ok {
					continue
				}
				for _, att := range row.Attachments {
					if att.SizeBytes > 25*1024*1024 {
						fmt.Fprintf(os.Stderr, "\n  ⚠  Skipping attachment %s (>25MB)\n", att.Name)
						if mlog != nil {
							mlog.AttachmentSkipped(sheetName, att.Name, "exceeds 25MB limit")
						}
						warnCount++
						continue
					}
					if err := downloadAndUpload(ctx, loader, sheetID, ssRowID, att); err != nil {
						fmt.Fprintf(os.Stderr, "\n  ⚠  Attachment %s failed: %v\n", att.Name, err)
						if mlog != nil {
							mlog.AttachmentFailed(sheetName, att.Name, err)
						}
						errCount++
					} else {
						attachCount++
					}
				}
			}
		}

		// Comments
		commentCount := 0
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
						if mlog != nil {
							mlog.CommentFailed(sheetName, err)
						}
						errCount++
					} else {
						commentCount++
					}
				}
			}
		}

		if mlog != nil {
			mlog.SheetComplete(sheetName, len(proj.Rows), attachCount, commentCount)
		}

		migState.MarkCompleted(proj.ID)
		_ = state.Save(stateFile, migState)
		_ = bar.Add(1)
	}

	// Final status
	status := "success"
	if !allSucceeded {
		status = "partial"
	}
	if mlog != nil {
		totalRows := 0
		for _, p := range allProjects {
			totalRows += len(p.Rows)
		}
		mlog.Summary(len(allProjects), totalRows, warnCount, errCount, status)
	}

	fmt.Println("\n✓ Migration complete!")
	if allSucceeded {
		_ = os.Remove(stateFile)
	} else {
		fmt.Printf("  State saved to %s for resume.\n", stateFile)
	}
	if mlog != nil {
		fmt.Printf("  📋 Log: %s\n", mlog.FilePath())
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
	excludeSet := make(map[string]bool, len(exclude))
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

// deduplicateProjectColumns renames duplicate column names in-place so that
// Smartsheet (which rejects sheets with two columns sharing the same name)
// does not return a 500 error. Both proj.Columns and every cell in proj.Rows
// are updated so that colMap lookups in BulkInsertRows remain consistent.
//
// Strategy: scan columns left-to-right. The first occurrence of a name keeps
// it. Subsequent occurrences get a "(2)", "(3)" … suffix. A mapping from
// each column index to its final name is used to rewrite cell ColumnName
// fields by matching cells to columns in order of occurrence.
//
// Example: two "Status" columns become "Status" and "Status (2)".
func deduplicateProjectColumns(proj *model.Project) {
	n := len(proj.Columns)
	if n == 0 {
		return
	}

	// Save original names before any mutation.
	origNames := make([]string, n)
	for i, c := range proj.Columns {
		origNames[i] = c.Name
	}

	// Assign final names; track all assigned names to avoid new collisions.
	assigned := make(map[string]bool, n)
	finalNames := make([]string, n)
	seenCount := make(map[string]int, n)

	for i, orig := range origNames {
		seenCount[orig]++
		if seenCount[orig] == 1 {
			finalNames[i] = orig
			assigned[orig] = true
			continue
		}
		counter := seenCount[orig]
		candidate := fmt.Sprintf("%s (%d)", orig, counter)
		for assigned[candidate] {
			counter++
			candidate = fmt.Sprintf("%s (%d)", orig, counter)
		}
		finalNames[i] = candidate
		assigned[candidate] = true
		proj.Columns[i].Name = candidate
	}

	// For each row, rewrite cells whose ColumnName is a duplicated original
	// name. We match cells to column occurrences in column-index order: the
	// first cell with name X goes to column occurrence 1 (keeps name X), the
	// second cell with name X goes to occurrence 2 (gets renamed), etc.
	// Build a lookup: origName → ordered list of final names.
	occurrencesByOrig := make(map[string][]string, n)
	for i, orig := range origNames {
		occurrencesByOrig[orig] = append(occurrencesByOrig[orig], finalNames[i])
	}

	for ri := range proj.Rows {
		// Per-row counter: how many cells with each original name we have seen.
		rowOccurrence := make(map[string]int)
		for ci := range proj.Rows[ri].Cells {
			name := proj.Rows[ri].Cells[ci].ColumnName
			finals, isDup := occurrencesByOrig[name]
			if !isDup || len(finals) <= 1 {
				continue // unique name — no rename needed
			}
			idx := rowOccurrence[name]
			rowOccurrence[name]++
			if idx < len(finals) {
				proj.Rows[ri].Cells[ci].ColumnName = finals[idx]
			}
		}
	}
}

var attachmentHTTPClient = &http.Client{Timeout: 60 * time.Second}

func downloadAndUpload(ctx context.Context, loader *ssloader.Loader, sheetID, rowID int64, att model.Attachment) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, nil)
	if err != nil {
		return err
	}
	resp, err := attachmentHTTPClient.Do(req)
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

// platformProjectIcon returns a small emoji representing a project on the given platform.
func platformProjectIcon(platform string) string {
	switch strings.ToLower(platform) {
	case "asana":
		return "🅰️"
	case "monday":
		return "📅"
	case "trello":
		return "📋"
	case "jira":
		return "🔷"
	case "airtable":
		return "🗂️"
	case "notion":
		return "📓"
	case "wrike":
		return "✍️"
	default:
		return "📌"
	}
}

func printSplash() {
	const (
		reset   = "\033[0m"
		bold    = "\033[1m"
		cyan    = "\033[1;36m"
		green   = "\033[1;32m"
		yellow  = "\033[1;33m"
		dim     = "\033[2m"
		white   = "\033[1;37m"
		bgCyan  = "\033[46m"
		fgBlack = "\033[30m"
	)

	fmt.Println()

	// Wordmark — clean, no box-drawing clutter
	fmt.Println(bold + cyan + `   ╔╦╗╦╔═╗╦═╗╔═╗╔╦╗╔═╗` + reset + `  ` + bold + fgBlack + bgCyan + ` → SMARTSHEET ` + reset)
	fmt.Println(bold + cyan + `   ║║║║║ ╦╠╦╝╠═╣ ║ ║╣ ` + reset)
	fmt.Println(bold + cyan + `   ╩ ╩╩╚═╝╩╚═╩ ╩ ╩ ╚═╝` + reset + `  ` + dim + `v` + version + reset)
	fmt.Println()

	// Tagline — short, punchy, tells you exactly what it does
	fmt.Println(white + bold + `   Migrate your projects into Smartsheet.` + reset)
	fmt.Println(dim + `   Asana · Monday · Trello · Jira · Airtable · Notion · Wrike` + reset)
	fmt.Println()

	// Trust signals — one line each, scannable
	fmt.Println(`   ` + green + `✓` + reset + dim + `  Non-destructive — your source data is never modified` + reset)
	fmt.Println(`   ` + green + `✓` + reset + dim + `  Resumable — interrupted? pick up where you left off` + reset)
	fmt.Println(`   ` + green + `✓` + reset + dim + `  Full fidelity — dates, dropdowns, contacts, attachments` + reset)
	fmt.Println()

	// Soft separator + attribution
	fmt.Println(`   ` + dim + `────────────────────────────────────────────────────` + reset)
	fmt.Println(`   ` + dim + yellow + `github.com/glitchedgod/migrate-to-smartsheet` + reset)
	fmt.Println()
}
