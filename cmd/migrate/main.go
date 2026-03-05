package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=x.y.z"
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "migrate-to-smartsheet",
		Short:   "Migrate data from project management tools into Smartsheet",
		Version: version,
		RunE:    runMigrate,
	}

	root.PersistentFlags().String("source", "", "Source platform (asana|monday|trello|jira|airtable|notion|wrike)")
	root.PersistentFlags().String("source-token", "", "Source platform API token")
	root.PersistentFlags().String("source-key", "", "Trello API key (Trello only)")
	root.PersistentFlags().String("smartsheet-token", "", "Smartsheet API token")
	root.PersistentFlags().String("workspace", "", "Source workspace name")
	root.PersistentFlags().String("projects", "", "Comma-separated project names (omit = all)")
	root.PersistentFlags().String("created-after", "", "Only migrate items created after this date (ISO 8601)")
	root.PersistentFlags().String("updated-after", "", "Only migrate items updated after this date (ISO 8601)")
	root.PersistentFlags().String("user-map", "", "Path to CSV file mapping source user IDs to Smartsheet emails")
	root.PersistentFlags().String("sheet-prefix", "", "Prefix to add to each created sheet name")
	root.PersistentFlags().String("sheet-suffix", "", "Suffix to add to each created sheet name")
	root.PersistentFlags().String("sheet-name-template", "{project}", "Sheet naming template: {source}, {project}, {date}")
	root.PersistentFlags().String("conflict", "skip", "Conflict handling: skip|rename|overwrite")
	root.PersistentFlags().String("exclude-fields", "", "Comma-separated field names to exclude")
	root.PersistentFlags().String("jira-email", "", "Jira account email (Jira only)")
	root.PersistentFlags().String("jira-instance", "", "Jira Cloud URL (e.g. https://yourorg.atlassian.net)")
	root.PersistentFlags().Bool("include-attachments", true, "Download and re-upload attachments")
	root.PersistentFlags().Bool("include-comments", true, "Migrate comments")
	root.PersistentFlags().Bool("include-archived", false, "Include archived/completed items")
	root.PersistentFlags().Bool("yes", false, "Skip confirmation prompt")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

