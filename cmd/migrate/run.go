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
	"github.com/glitchedgod/migrate-to-smartsheet/internal/postmigration"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/preview"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/state"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/wizard"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
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
	} else if len(workspaces) == 1 || yes {
		// With --yes and multiple workspaces, default to the first one.
		// Users can specify --workspace to target a specific one.
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
	wsPermalink := ""
	// For Jira, use a shorter display name derived from the instance URL
	if sourceStr == "jira" {
		ssWorkspaceName = strings.TrimPrefix(strings.TrimPrefix(selectedWorkspace.Name, "https://"), "http://")
	}
	// For Airtable, clarify that this is a Base (matches user mental model)
	if sourceStr == "airtable" {
		ssWorkspaceName = selectedWorkspace.Name + " (Airtable)"
	}
	if ssWorkspaceName != "" {
		wsID, wsLink, wsErr := loader.CreateWorkspace(ctx, ssWorkspaceName)
		if wsErr != nil {
			fmt.Fprintf(os.Stderr, "  ⚠  Could not create Smartsheet workspace %q: %v — sheets will be created at root\n", ssWorkspaceName, wsErr)
		} else {
			ssWorkspaceID = wsID
			wsPermalink = wsLink
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

	// Compute sheet names in the same order as allProjects (needed for mapping preview).
	sheetNames := make([]string, len(allProjects))
	for i, proj := range allProjects {
		sheetNames[i] = applySheetNaming(proj.Name, sourceStr, sheetPrefix, sheetSuffix, sheetTemplate)
	}

	// Preview
	previewWorkspaces := []model.Workspace{{ID: selectedWorkspace.ID, Name: selectedWorkspace.Name, Projects: allProjects}}
	summary := preview.Summarize(previewWorkspaces)

	printMappingPreview(sourceStr, selectedWorkspace.Name, ssWorkspaceName, allProjects, sheetNames, summary)

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

	// Fetch existing sheet names once so conflict resolution can look them up
	// without an API call per project.
	existingSheets := map[string]int64{}
	if conflictMode == "rename" || conflictMode == "overwrite" {
		if m, err := loader.ListSheetNames(ctx, ssWorkspaceID); err == nil {
			existingSheets = m
		} else {
			fmt.Fprintf(os.Stderr, "  ⚠  Could not list existing sheets (conflict=%s will be best-effort): %v\n", conflictMode, err)
		}
	}

	// Migrate — per-project live status
	tracker := newStatusTracker(sheetNames)
	fmt.Println()
	fmt.Println("  ─────────────────────────────────────────────────────────────────")

	allSucceeded := true
	errCount := 0
	warnCount := len(summary.Warnings)
	outcomes := make([]postmigration.ProjectOutcome, 0, len(allProjects))

	for i := range allProjects {
		proj := &allProjects[i]
		tracker.start(i)

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

		// Apply sheet naming (already computed in sheetNames[i], update proj.Name to match)
		sheetName := sheetNames[i]
		proj.Name = sheetName

		// Deduplicate column names before creating the sheet.
		// Smartsheet returns HTTP 500 when two columns share the same name.
		deduplicateProjectColumns(proj)

		// Conflict handling
		switch conflictMode {
		case "rename":
			if _, exists := existingSheets[sheetName]; exists {
				sheetName = resolveRename(sheetName, existingSheets)
				proj.Name = sheetName
				sheetNames[i] = sheetName
			}
		case "overwrite":
			if existingID, exists := existingSheets[sheetName]; exists {
				if err := loader.DeleteSheet(ctx, existingID); err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠  Could not delete existing sheet %q for overwrite: %v\n", sheetName, err)
				} else {
					delete(existingSheets, sheetName)
				}
			}
		// "skip" (default): do nothing — CreateSheet will return an error if the sheet exists
		}

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
			msg := fmt.Sprintf("sheet create failed: %v", err)
			tracker.fail(i, msg)
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to create sheet %s: %v\n", sheetName, err)
			if mlog != nil {
				mlog.SheetFailed(sheetName, err)
			}
			allSucceeded = false
			errCount++
			outcomes = append(outcomes, postmigration.ProjectOutcome{
				SourceID:  proj.ID,
				Name:      proj.Name,
				SheetName: sheetName,
				Err:       err,
			})
			continue
		}

		if mlog != nil {
			mlog.Info("sheet created",
				"sheet", sheetName,
				"sheet_id", sheetID,
				"smartsheet_workspace_id", ssWorkspaceID,
			)
		}
		// Track newly created sheet so subsequent projects in the same run
		// can detect name collisions (relevant for rename/overwrite modes).
		existingSheets[sheetName] = sheetID

		rowIDMap, rowErr := loader.BulkInsertRows(ctx, sheetID, proj.Rows, colMap, contactColIDs)
		if rowErr != nil {
			msg := fmt.Sprintf("row insert failed: %v", rowErr)
			tracker.fail(i, msg)
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to insert rows for %s: %v\n", sheetName, rowErr)
			if mlog != nil {
				mlog.Error("row insert failed",
					"sheet", sheetName,
					"sheet_id", sheetID,
					"error", rowErr.Error(),
				)
			}
			allSucceeded = false
			errCount++
			outcomes = append(outcomes, postmigration.ProjectOutcome{
				SourceID:  proj.ID,
				Name:      proj.Name,
				SheetName: sheetName,
				SheetID:   sheetID,
				Err:       rowErr,
			})
			continue
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

		noun := platformNoun(sourceStr)
		completeMsg := fmt.Sprintf("%d %s", len(proj.Rows), noun)
		if attachCount > 0 {
			completeMsg += fmt.Sprintf(" · %d attachments", attachCount)
		}
		tracker.complete(i, completeMsg)

		migState.MarkCompleted(proj.ID)
		_ = state.Save(stateFile, migState)

		outcomes = append(outcomes, postmigration.ProjectOutcome{
			SourceID:     proj.ID,
			Name:         proj.Name,
			SheetName:    sheetName,
			SheetID:      sheetID,
			RowCount:     len(proj.Rows),
			AttachCount:  attachCount,
			CommentCount: commentCount,
		})
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

	// Print completion summary
	tracker.printAll()

	// State file cleanup
	if allSucceeded {
		_ = os.Remove(stateFile)
	}

	// Build MigrationResult and launch post-migration menu
	logPath := ""
	if mlog != nil {
		logPath = mlog.FilePath()
	}
	resultStateFile := ""
	if !allSucceeded {
		resultStateFile = stateFile
	}
	result := postmigration.MigrationResult{
		Source:             sourceStr,
		Outcomes:           outcomes,
		WorkspacePermalink: wsPermalink,
		WorkspaceName:      ssWorkspaceName,
		LogPath:            logPath,
		StateFile:          resultStateFile,
		Rerunner: func(failed []postmigration.ProjectOutcome) []postmigration.ProjectOutcome {
			return runFailedProjects(ctx, failed, ext, loader, sourceStr, selectedWorkspace.ID,
				ssWorkspaceID, ssWorkspaceName, opts, userMap, sheetPrefix, sheetSuffix, sheetTemplate,
				includeAttachments, includeComments, conflictMode, migState, stateFile, mlog)
		},
	}
	postmigration.RunMenu(result)

	return nil
}

// ── Mapping preview ────────────────────────────────────────────────────────────

// platformNoun returns the domain noun for items in the given platform.
func platformNoun(platform string) string {
	switch strings.ToLower(platform) {
	case "asana", "wrike":
		return "tasks"
	case "monday":
		return "items"
	case "trello":
		return "cards"
	case "jira":
		return "issues"
	case "airtable":
		return "records"
	case "notion":
		return "pages"
	default:
		return "rows"
	}
}

// printMappingPreview renders the source → destination mapping before asking
// the user to confirm the migration.
func printMappingPreview(source, workspaceName, ssWorkspaceName string, projects []model.Project, sheetNames []string, summary preview.Summary) {
	const sep = "  ─────────────────────────────────────────────────────────────────"
	noun := platformNoun(source)
	icon := platformProjectIcon(source)

	// Build a per-project warning lookup from summary.Warnings.
	// summary.Warnings are global strings; we do a best-effort substring match
	// to attribute them to specific projects.
	fmt.Println()
	fmt.Println("  📦  Migration Map")
	fmt.Println(sep)
	fmt.Printf("  📁  %s  →  Smartsheet: %s\n\n", workspaceName, ssWorkspaceName)

	totalRows := 0
	totalAttach := 0
	transferableAttach := 0

	for i, proj := range projects {
		sheetName := ""
		if i < len(sheetNames) {
			sheetName = sheetNames[i]
		}

		rowCount := len(proj.Rows)
		totalRows += rowCount

		// Count attachments and oversized ones for this project.
		attCount := 0
		oversized := 0
		hasContact := false
		for _, col := range proj.Columns {
			if col.Type == model.TypeContact || col.Type == model.TypeMultiContact {
				hasContact = true
				break
			}
		}
		for _, row := range proj.Rows {
			for _, att := range row.Attachments {
				attCount++
				if att.SizeBytes > 25*1024*1024 {
					oversized++
				}
			}
		}
		totalAttach += attCount
		transferableAttach += attCount - oversized

		fmt.Printf("      %s  %s\n", icon, proj.Name)
		fmt.Printf("          → Sheet %q\n", sheetName)

		// Build the detail line
		detailParts := []string{fmt.Sprintf("%d %s", rowCount, noun)}
		if attCount > 0 {
			detailParts = append(detailParts, fmt.Sprintf("%d attachments", attCount))
		}
		if hasContact {
			detailParts = append(detailParts, "contact columns")
		}
		fmt.Printf("             %s\n", strings.Join(detailParts, " · "))

		// Per-project warning for oversized attachments
		if oversized > 0 {
			fmt.Printf("             ⚠  %d attachment(s) >25MB will be skipped\n", oversized)
		}
		fmt.Println()
	}

	fmt.Println(sep)

	// Summary line
	summaryParts := []string{
		fmt.Sprintf("%d sheets", len(projects)),
		fmt.Sprintf("%d %s", totalRows, noun),
	}
	if totalAttach > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d attachments transferable", transferableAttach))
	}
	if len(summary.Warnings) > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d warnings", len(summary.Warnings)))
	}
	fmt.Printf("  %s\n", strings.Join(summaryParts, " · "))
}

// ── Per-project live status tracker ───────────────────────────────────────────

type projectStatus int

const (
	statusPending   projectStatus = iota
	statusMigrating               //nolint:deadcode,varcheck
	statusDone
	statusFailed
)

type statusTracker struct {
	names    []string
	statuses []projectStatus
	msgs     []string
	total    int
}

func newStatusTracker(names []string) *statusTracker {
	t := &statusTracker{
		names:    names,
		statuses: make([]projectStatus, len(names)),
		msgs:     make([]string, len(names)),
		total:    len(names),
	}
	return t
}

// start prints the "currently migrating" line for project i.
func (t *statusTracker) start(i int) {
	t.statuses[i] = statusMigrating
	fmt.Printf("  ⟳  %s...\n", t.names[i])
}

// complete marks project i as done and prints its completion line.
func (t *statusTracker) complete(i int, msg string) {
	t.statuses[i] = statusDone
	t.msgs[i] = msg
}

// fail marks project i as failed and prints the failure line immediately.
func (t *statusTracker) fail(i int, msg string) {
	t.statuses[i] = statusFailed
	t.msgs[i] = msg
}

// printAll prints the final per-project summary table.
func (t *statusTracker) printAll() {
	const sep = "  ─────────────────────────────────────────────────────────────────"
	const (
		ansiReset = "\033[0m"
		ansiRed   = "\033[31m"
		ansiGreen = "\033[32m"
	)

	fmt.Println()
	fmt.Println(sep)
	succeeded := 0
	failed := 0
	for i, name := range t.names {
		switch t.statuses[i] {
		case statusDone:
			succeeded++
			if t.msgs[i] != "" {
				fmt.Printf("  %s✓%s  %-40s  %s\n", ansiGreen, ansiReset, name, t.msgs[i])
			} else {
				fmt.Printf("  %s✓%s  %s\n", ansiGreen, ansiReset, name)
			}
		case statusFailed:
			failed++
			if t.msgs[i] != "" {
				fmt.Printf("  %s✗%s  %-40s  %s\n", ansiRed, ansiReset, name, t.msgs[i])
			} else {
				fmt.Printf("  %s✗%s  %s\n", ansiRed, ansiReset, name)
			}
		default:
			fmt.Printf("  ·  %s\n", name)
		}
	}
	fmt.Println()
	fmt.Printf("  Migration complete  ·  %d of %d sheets\n", succeeded, t.total)
	fmt.Println(sep)
}

// runFailedProjects re-extracts and re-migrates only the projects that failed
// in the original run. It clears those project IDs from the state file first
// so they are treated as not yet migrated, then runs the same migration loop.
// Returns updated ProjectOutcomes (one per failed project).
func runFailedProjects(
	ctx context.Context,
	failed []postmigration.ProjectOutcome,
	ext extractor.Extractor,
	loader *ssloader.Loader,
	sourceStr, workspaceID string,
	ssWorkspaceID int64,
	ssWorkspaceName string,
	opts extractor.Options,
	userMap *transformer.UserMap,
	sheetPrefix, sheetSuffix, sheetTemplate string,
	includeAttachments, includeComments bool,
	conflictMode string,
	migState *state.MigrationState,
	stateFile string,
	mlog *miglog.Logger,
) []postmigration.ProjectOutcome {
	// Clear failed IDs from state so they re-run.
	for _, f := range failed {
		migState.ClearCompleted(f.SourceID)
	}
	_ = state.Save(stateFile, migState)

	sheetNames := make([]string, len(failed))
	projects := make([]model.Project, 0, len(failed))

	for i, f := range failed {
		extracted, err := ext.ExtractProject(ctx, workspaceID, f.SourceID, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠  Re-extract failed for %s: %v\n", f.Name, err)
			// Return a failed outcome immediately — no project to migrate.
			failed[i].Err = err
			sheetNames[i] = f.SheetName
			continue
		}
		if len(opts.ExcludeFields) > 0 {
			extracted = applyExcludeFields(extracted, opts.ExcludeFields)
		}
		sheetNames[i] = applySheetNaming(extracted.Name, sourceStr, sheetPrefix, sheetSuffix, sheetTemplate)
		projects = append(projects, *extracted)
	}

	tracker := newStatusTracker(sheetNames)
	fmt.Println()
	fmt.Println("  ─────────────────────────────────────────────────────────────────")

	outcomes := make([]postmigration.ProjectOutcome, len(failed))
	// Pre-fill with the existing failed outcomes so extraction failures carry through.
	copy(outcomes, failed)

	projIdx := 0
	for i := range failed {
		if projIdx >= len(projects) || projects[projIdx].ID != failed[i].SourceID {
			// This project failed to extract — skip the loop body.
			tracker.fail(i, fmt.Sprintf("extract failed: %v", failed[i].Err))
			continue
		}
		proj := &projects[projIdx]
		projIdx++
		tracker.start(i)

		colTypeByName := make(map[string]model.ColumnType, len(proj.Columns))
		for _, col := range proj.Columns {
			colTypeByName[col.Name] = col.Type
		}
		for ri := range proj.Rows {
			for ci := range proj.Rows[ri].Cells {
				cell := &proj.Rows[ri].Cells[ci]
				cell.Value = transformer.TransformCellValue(cell.Value, colTypeByName[cell.ColumnName], userMap)
			}
		}

		sheetName := sheetNames[i]
		proj.Name = sheetName
		deduplicateProjectColumns(proj)
		_ = conflictMode

		sheetID, colMap, contactColIDs, err := loader.CreateSheet(ctx, proj, ssWorkspaceID)
		if err != nil {
			tracker.fail(i, fmt.Sprintf("sheet create failed: %v", err))
			outcomes[i] = postmigration.ProjectOutcome{SourceID: proj.ID, Name: proj.Name, SheetName: sheetName, Err: err}
			continue
		}

		rowIDMap, rowErr := loader.BulkInsertRows(ctx, sheetID, proj.Rows, colMap, contactColIDs)
		if rowErr != nil {
			tracker.fail(i, fmt.Sprintf("row insert failed: %v", rowErr))
			outcomes[i] = postmigration.ProjectOutcome{SourceID: proj.ID, Name: proj.Name, SheetName: sheetName, SheetID: sheetID, Err: rowErr}
			continue
		}

		attachCount := 0
		if includeAttachments {
			for _, row := range proj.Rows {
				ssRowID, ok := rowIDMap[row.ID]
				if !ok {
					continue
				}
				for _, att := range row.Attachments {
					if att.SizeBytes > 25*1024*1024 {
						continue
					}
					if err := downloadAndUpload(ctx, loader, sheetID, ssRowID, att); err == nil {
						attachCount++
					}
				}
			}
		}

		commentCount := 0
		if includeComments {
			for _, row := range proj.Rows {
				ssRowID, ok := rowIDMap[row.ID]
				if !ok {
					continue
				}
				for _, comment := range row.Comments {
					text := fmt.Sprintf("[%s] %s", comment.AuthorName, comment.Body)
					if err := loader.AddComment(ctx, sheetID, ssRowID, text); err == nil {
						commentCount++
					}
				}
			}
		}

		noun := platformNoun(sourceStr)
		msg := fmt.Sprintf("%d %s", len(proj.Rows), noun)
		if attachCount > 0 {
			msg += fmt.Sprintf(" · %d attachments", attachCount)
		}
		tracker.complete(i, msg)

		migState.MarkCompleted(proj.ID)
		_ = state.Save(stateFile, migState)

		outcomes[i] = postmigration.ProjectOutcome{
			SourceID:     proj.ID,
			Name:         proj.Name,
			SheetName:    sheetName,
			SheetID:      sheetID,
			RowCount:     len(proj.Rows),
			AttachCount:  attachCount,
			CommentCount: commentCount,
		}

		if mlog != nil {
			mlog.SheetComplete(sheetName, len(proj.Rows), attachCount, commentCount)
		}
	}

	tracker.printAll()

	// If everything succeeded now, clean up state file.
	allOK := true
	for _, o := range outcomes {
		if o.Err != nil {
			allOK = false
			break
		}
	}
	if allOK {
		_ = os.Remove(stateFile)
	}

	return outcomes
}

// resolveRename appends " (2)", " (3)" … to name until a name not present in
// existing is found, then returns it.
func resolveRename(name string, existing map[string]int64) string {
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", name, i)
		if _, taken := existing[candidate]; !taken {
			return candidate
		}
	}
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

	// Smartsheet compares column titles case-insensitively, so we normalise to
	// lowercase for all collision checks while preserving display casing.
	lower := func(s string) string { return strings.ToLower(s) }

	// Assign final names; track all assigned names (lowercased) to avoid new collisions.
	assignedLower := make(map[string]bool, n)
	finalNames := make([]string, n)
	seenCountLower := make(map[string]int, n)

	for i, orig := range origNames {
		key := lower(orig)
		seenCountLower[key]++
		if seenCountLower[key] == 1 {
			finalNames[i] = orig
			assignedLower[key] = true
			continue
		}
		counter := seenCountLower[key]
		candidate := fmt.Sprintf("%s (%d)", orig, counter)
		for assignedLower[lower(candidate)] {
			counter++
			candidate = fmt.Sprintf("%s (%d)", orig, counter)
		}
		finalNames[i] = candidate
		assignedLower[lower(candidate)] = true
		proj.Columns[i].Name = candidate
	}

	// For each row, rewrite cells whose ColumnName is a duplicated original
	// name. We match cells to column occurrences in column-index order: the
	// first cell with name X goes to column occurrence 1 (keeps name X), the
	// second cell with name X goes to occurrence 2 (gets renamed), etc.
	// Build a lookup: lower(origName) → ordered list of final names.
	occurrencesByOrig := make(map[string][]string, n)
	for i, orig := range origNames {
		occurrencesByOrig[lower(orig)] = append(occurrencesByOrig[lower(orig)], finalNames[i])
	}

	for ri := range proj.Rows {
		// Per-row counter: how many cells with each original name we have seen.
		rowOccurrence := make(map[string]int)
		for ci := range proj.Rows[ri].Cells {
			key := lower(proj.Rows[ri].Cells[ci].ColumnName)
			finals, isDup := occurrencesByOrig[key]
			if !isDup || len(finals) <= 1 {
				continue // unique name — no rename needed
			}
			idx := rowOccurrence[key]
			rowOccurrence[key]++
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
