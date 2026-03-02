package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	survey "github.com/AlecAivazis/survey/v2"
	airtableext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/airtable"
	asanaext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/asana"
	jiraext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/jira"
	mondayext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/monday"
	notionext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/notion"
	trelloext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/trello"
	wrikeext "github.com/bchauhan/migrate-to-smartsheet/internal/extractor/wrike"
	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
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
		sheetID, colMap, err := loader.CreateSheet(ctx, &proj, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  ⚠  Failed to create sheet %s: %v\n", proj.Name, err)
			continue
		}
		if err := loader.BulkInsertRows(ctx, sheetID, proj.Rows, colMap); err != nil {
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
