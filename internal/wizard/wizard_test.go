package wizard_test

import (
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/wizard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryHasAllPlatforms(t *testing.T) {
	platforms := []string{"asana", "monday", "trello", "jira", "airtable", "notion", "wrike", "smartsheet"}
	for _, p := range platforms {
		t.Run(p, func(t *testing.T) {
			wiz, ok := wizard.Registry[p]
			require.True(t, ok, "platform %q not found in Registry", p)
			assert.NotEmpty(t, wiz.DisplayName)
			assert.NotEmpty(t, wiz.Icon)
			assert.NotEmpty(t, wiz.Steps)
			assert.NotEmpty(t, wiz.Prompts)
		})
	}
}

func TestTrelloHasTwoPrompts(t *testing.T) {
	wiz := wizard.Registry["trello"]
	assert.Len(t, wiz.Prompts, 2)
	assert.Equal(t, "key", wiz.Prompts[0].Field)
	assert.Equal(t, "token", wiz.Prompts[1].Field)
}

func TestJiraHasThreePrompts(t *testing.T) {
	wiz := wizard.Registry["jira"]
	assert.Len(t, wiz.Prompts, 3)
	fields := make([]string, len(wiz.Prompts))
	for i, p := range wiz.Prompts {
		fields[i] = p.Field
	}
	assert.Equal(t, []string{"instance", "email", "token"}, fields)
	// instance and email are visible (plain text, not masked)
	assert.True(t, wiz.Prompts[0].IsPlain, "instance should be plain text")
	assert.True(t, wiz.Prompts[1].IsPlain, "email should be plain text")
	assert.False(t, wiz.Prompts[2].IsPlain, "token should be masked")
}
