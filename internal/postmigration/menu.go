// Package postmigration provides the interactive post-migration menu, log
// viewer, and GitHub error-reporting helpers shown after a migration run.
package postmigration

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
)

// ProjectOutcome holds the result of migrating one source project.
type ProjectOutcome struct {
	SourceID     string
	Name         string
	SheetName    string
	SheetID      int64
	RowCount     int
	AttachCount  int
	CommentCount int
	Err          error
}

// MigrationResult is the top-level result passed to RunMenu after the
// migration loop completes.
type MigrationResult struct {
	Source             string
	Outcomes           []ProjectOutcome
	WorkspacePermalink string // may be empty
	WorkspaceName      string
	LogPath            string
	StateFile          string // path to state file; empty if all succeeded
	// Rerunner is called when the user picks "Re-run failed sheets". It
	// receives the failed outcomes and returns updated outcomes after the
	// retry. If nil, the option is disabled.
	Rerunner func(failed []ProjectOutcome) []ProjectOutcome
}

// hasErrors reports whether any outcome carried an error.
func hasErrors(outcomes []ProjectOutcome) bool {
	for _, o := range outcomes {
		if o.Err != nil {
			return true
		}
	}
	return false
}

// collectErrors returns the error strings from all failed outcomes.
func collectErrors(outcomes []ProjectOutcome) []string {
	var out []string
	for _, o := range outcomes {
		if o.Err != nil {
			out = append(out, fmt.Sprintf("[sheet %q] %s", o.SheetName, o.Err.Error()))
		}
	}
	return out
}

// RunMenu presents the interactive post-migration menu and loops until the
// user selects Exit.
func RunMenu(result MigrationResult) {
	withErrors := hasErrors(result.Outcomes)

	for {
		options := buildOptions(result, withErrors)

		var choice string
		prompt := &survey.Select{
			Message: "What would you like to do next?",
			Options: options,
		}
		if err := survey.AskOne(prompt, &choice); err != nil {
			// Ctrl-C or EOF — treat as exit.
			return
		}

		switch choice {
		case optOpenBrowser:
			if result.WorkspacePermalink == "" {
				fmt.Println("  No workspace permalink available.")
				continue
			}
			if err := OpenBrowser(result.WorkspacePermalink); err != nil {
				fmt.Printf("  Could not open browser: %v\n", err)
			} else {
				fmt.Printf("  Opened %s\n", result.WorkspacePermalink)
			}

		case optViewLog:
			if result.LogPath == "" {
				fmt.Println("  No log file path recorded.")
				continue
			}
			PrintLogSummary(result.LogPath)

		case optReportErrors:
			errs := collectErrors(result.Outcomes)
			url := BuildGitHubIssueURL(result.Source, errs)
			if err := OpenBrowser(url); err != nil {
				fmt.Printf("  Could not open browser: %v\n", err)
				fmt.Printf("  Open manually: %s\n", url)
			} else {
				fmt.Println("  Opened GitHub issue form in your browser.")
				fmt.Println("  github.com/glitchedgod/migrate-to-smartsheet/issues/new")
			}

		case optRerun:
			if result.Rerunner == nil {
				fmt.Println("  Re-run is not available.")
				continue
			}
			failed := failedOutcomes(result.Outcomes)
			fmt.Printf("\n  Re-running %d failed sheet(s)...\n\n", len(failed))
			retried := result.Rerunner(failed)
			// Merge retried outcomes back: replace each matching SourceID.
			result.Outcomes = mergeOutcomes(result.Outcomes, retried)
			withErrors = hasErrors(result.Outcomes)
			if !withErrors {
				fmt.Println("\n  All sheets migrated successfully.")
			}

		case optExit:
			return
		}
	}
}

// failedOutcomes returns only the outcomes that carried an error.
func failedOutcomes(outcomes []ProjectOutcome) []ProjectOutcome {
	var out []ProjectOutcome
	for _, o := range outcomes {
		if o.Err != nil {
			out = append(out, o)
		}
	}
	return out
}

// mergeOutcomes replaces entries in base with the corresponding entry from
// updates (matched by SourceID), preserving the original order.
func mergeOutcomes(base, updates []ProjectOutcome) []ProjectOutcome {
	updateMap := make(map[string]ProjectOutcome, len(updates))
	for _, u := range updates {
		updateMap[u.SourceID] = u
	}
	result := make([]ProjectOutcome, len(base))
	for i, o := range base {
		if u, ok := updateMap[o.SourceID]; ok {
			result[i] = u
		} else {
			result[i] = o
		}
	}
	return result
}

// Menu option labels — kept as constants so the switch stays in sync with the
// slice built by buildOptions.
const (
	optOpenBrowser  = "\U0001F680  Open Smartsheet workspace in browser"
	optViewLog      = "\U0001F4CB  View migration log"
	optReportErrors = "\U0001F41B  Report errors to GitHub"
	optRerun        = "\U0001F504  Re-run failed sheets"
	optExit         = "\U0001F44B  Exit"
)

// buildOptions constructs the ordered list of menu choices, omitting
// context-dependent options when they are not applicable.
func buildOptions(result MigrationResult, withErrors bool) []string {
	var opts []string

	if result.WorkspacePermalink != "" {
		opts = append(opts, optOpenBrowser)
	}
	if result.LogPath != "" {
		opts = append(opts, optViewLog)
	}
	if withErrors {
		opts = append(opts, optReportErrors)
		if result.Rerunner != nil {
			opts = append(opts, optRerun)
		}
	}
	opts = append(opts, optExit)

	return opts
}
