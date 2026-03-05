package wizard

import (
	"fmt"
	"strings"

	survey "github.com/AlecAivazis/survey/v2"
)

// ANSI color helpers using raw escape codes (no extra deps).
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	cyan   = "\033[1;36m"
	yellow = "\033[1;33m"
	green  = "\033[1;32m"
	dim    = "\033[2m"
	red    = "\033[1;31m"
)

func colorize(code, s string) string { return code + s + reset }

// CredentialResult holds all credentials collected for any platform.
type CredentialResult struct {
	Token    string // primary API token / Personal Access Token
	Key      string // Trello API key
	Email    string // Jira account email
	Instance string // Jira Cloud instance URL
}

// Step is one numbered instruction line shown to the user.
type Step struct {
	Text   string
	IsURL  bool // rendered as a clickable URL line
	IsWarn bool // rendered as a warning
}

// Prompt describes a single credential field to collect.
type Prompt struct {
	Field   string // "token" | "key" | "email" | "instance"
	Message string // label shown to user
	IsPlain bool   // use Input (visible) instead of Password (masked)
}

// PlatformWizard is the complete config for one source platform.
type PlatformWizard struct {
	DisplayName string
	Icon        string
	Steps       []Step
	Prompts     []Prompt
}

// Registry maps lowercase platform name → wizard config.
var Registry = map[string]PlatformWizard{
	"asana": {
		DisplayName: "Asana",
		Icon:        "🅰️ ",
		Steps: []Step{
			{Text: "Open https://app.asana.com/0/my-tasks/list in your browser", IsURL: true},
			{Text: "Click your profile photo (top-right) → My Settings"},
			{Text: "Select the Apps tab"},
			{Text: "Under Personal Access Tokens, click + New access token"},
			{Text: "Give it a name (e.g. \"migrate-to-smartsheet\") and click Create token"},
			{Text: "Copy the token — it is shown only once ⚠️", IsWarn: true},
		},
		Prompts: []Prompt{
			{Field: "token", Message: "Paste your Asana Personal Access Token"},
		},
	},
	"monday": {
		DisplayName: "Monday.com",
		Icon:        "📅",
		Steps: []Step{
			{Text: "Click your avatar (top-right corner) to open the account menu"},
			{Text: "Navigate to Administration → Connections → API"},
			{Text: "Find the Personal API Token (v2) section"},
			{Text: "Click Copy to clipboard"},
		},
		Prompts: []Prompt{
			{Field: "token", Message: "Paste your Monday.com Personal API Token (v2)"},
		},
	},
	"trello": {
		DisplayName: "Trello",
		Icon:        "📋",
		Steps: []Step{
			{Text: "── Step 1: Get your API Key ─────────────────────────"},
			{Text: "Open https://trello.com/power-ups/admin in your browser", IsURL: true},
			{Text: "Click New (top-right), create a Power-Up to get an API Key"},
			{Text: "Copy the API Key shown on the dashboard"},
			{Text: "── Step 2: Get your OAuth Token ─────────────────────"},
			{Text: "Open: https://trello.com/1/authorize?expiration=never&name=migrate-to-smartsheet&scope=read&response_type=token&key=YOUR_API_KEY", IsURL: true},
			{Text: "Replace YOUR_API_KEY in the URL above with the key you just copied", IsWarn: true},
			{Text: "Click Allow and copy the 64-character token shown"},
		},
		Prompts: []Prompt{
			{Field: "key",   Message: "Paste your Trello API Key", IsPlain: true},
			{Field: "token", Message: "Paste your Trello OAuth Token"},
		},
	},
	"jira": {
		DisplayName: "Jira",
		Icon:        "🔷",
		Steps: []Step{
			{Text: "Your Jira Cloud URL is the base URL you use to log in"},
			{Text: "e.g. https://yourorg.atlassian.net  (not the full page URL)", IsURL: true},
			{Text: "Open https://id.atlassian.com/manage-profile/security/api-tokens", IsURL: true},
			{Text: "Click Create API token, give it a label, click Create"},
			{Text: "Copy the token — it is shown only once ⚠️", IsWarn: true},
		},
		Prompts: []Prompt{
			{Field: "instance", Message: "Jira Cloud URL (e.g. https://yourorg.atlassian.net)", IsPlain: true},
			{Field: "email",    Message: "Your Jira account email address", IsPlain: true},
			{Field: "token",    Message: "Paste your Jira API Token"},
		},
	},
	"airtable": {
		DisplayName: "Airtable",
		Icon:        "🗂️ ",
		Steps: []Step{
			{Text: "Open https://airtable.com/create/tokens in your browser", IsURL: true},
			{Text: "Click + Add a token"},
			{Text: "Under Scopes add:  data.records:read  and  schema.bases:read"},
			{Text: "Under Access choose All current and future bases (or select specific ones)"},
			{Text: "Click Create token and copy the generated token"},
		},
		Prompts: []Prompt{
			{Field: "token", Message: "Paste your Airtable Personal Access Token"},
		},
	},
	"notion": {
		DisplayName: "Notion",
		Icon:        "📓",
		Steps: []Step{
			{Text: "Open https://www.notion.so/my-integrations in your browser", IsURL: true},
			{Text: "Click + New integration"},
			{Text: "Give it a name, select your workspace, set Capabilities to Read content"},
			{Text: "Click Submit, then show the Internal Integration Secret and copy it"},
			{Text: "⚠️  For each database to migrate: open it → Share (top-right) → Invite your integration", IsWarn: true},
		},
		Prompts: []Prompt{
			{Field: "token", Message: "Paste your Notion Internal Integration Secret"},
		},
	},
	"wrike": {
		DisplayName: "Wrike",
		Icon:        "✍️ ",
		Steps: []Step{
			{Text: "Click your profile picture (top-right) in Wrike"},
			{Text: "Go to Apps & Integrations → API"},
			{Text: "Click Create new app, give it a name"},
			{Text: "Under the app, click Generate permanent token"},
			{Text: "Copy the token shown"},
		},
		Prompts: []Prompt{
			{Field: "token", Message: "Paste your Wrike Permanent Token"},
		},
	},
	"smartsheet": {
		DisplayName: "Smartsheet",
		Icon:        "📊",
		Steps: []Step{
			{Text: "Click Account (top-right avatar) in Smartsheet"},
			{Text: "Navigate to Personal Settings → API Access"},
			{Text: "Click Generate new access token, give it a name"},
			{Text: "Copy the token shown — it is shown only once ⚠️", IsWarn: true},
		},
		Prompts: []Prompt{
			{Field: "token", Message: "Paste your Smartsheet API Access Token"},
		},
	},
}

// ShowAndPrompt displays the setup wizard for the given platform and collects
// credentials interactively. Falls back to a bare password prompt if the
// platform is not in the registry.
func ShowAndPrompt(platform string) (CredentialResult, error) {
	wiz, ok := Registry[strings.ToLower(platform)]
	if !ok {
		var result CredentialResult
		err := survey.AskOne(
			&survey.Password{Message: fmt.Sprintf("[%s] API token:", platform)},
			&result.Token,
		)
		return result, err
	}

	// Banner
	width := 56
	fmt.Println()
	fmt.Println(colorize(bold, "┌"+strings.Repeat("─", width)+"┐"))
	title := fmt.Sprintf("  %s  %s  %sAPI Key Setup Guide%s",
		wiz.Icon, colorize(cyan, wiz.DisplayName), colorize(dim, ""), reset)
	fmt.Printf(colorize(bold, "│")+"%s\n", title)
	fmt.Println(colorize(bold, "└"+strings.Repeat("─", width)+"┘"))
	fmt.Println()

	// Numbered steps
	stepNum := 1
	for _, s := range wiz.Steps {
		// Section dividers (start with ──) are printed dimmed, not numbered
		if strings.HasPrefix(s.Text, "──") {
			fmt.Printf("  %s\n", colorize(dim, s.Text))
			continue
		}
		prefix := colorize(yellow, fmt.Sprintf("%d.", stepNum))
		stepNum++
		if s.IsURL {
			fmt.Printf("  %s  %s\n", prefix, colorize(green, "🔗 "+s.Text))
		} else if s.IsWarn {
			fmt.Printf("  %s  %s\n", prefix, colorize(red, s.Text))
		} else {
			fmt.Printf("  %s  %s\n", prefix, s.Text)
		}
	}

	fmt.Println()
	fmt.Println(colorize(dim, "  "+strings.Repeat("─", width)))
	fmt.Println()

	// Collect credentials
	var result CredentialResult
	for _, p := range wiz.Prompts {
		var val string
		var err error
		msg := "🔑 " + p.Message + ":"
		if p.IsPlain {
			err = survey.AskOne(&survey.Input{Message: msg}, &val)
		} else {
			err = survey.AskOne(&survey.Password{Message: msg}, &val)
		}
		if err != nil {
			return result, err
		}
		switch p.Field {
		case "token":
			result.Token = val
		case "key":
			result.Key = val
		case "email":
			result.Email = val
		case "instance":
			result.Instance = val
		}
	}
	fmt.Println()
	return result, nil
}
