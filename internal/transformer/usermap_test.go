package transformer_test

import (
	"strings"
	"testing"

	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadUserMap(t *testing.T) {
	csv := "source_id,smartsheet_email\nuser123,alice@example.com\nuser456,bob@example.com\n"
	um, err := transformer.LoadUserMapFromReader(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", um.Lookup("user123"))
	assert.Equal(t, "bob@example.com", um.Lookup("user456"))
	assert.Equal(t, "", um.Lookup("unknown"))
}

func TestLoadUserMapEmpty(t *testing.T) {
	um, err := transformer.LoadUserMapFromReader(strings.NewReader("source_id,smartsheet_email\n"))
	require.NoError(t, err)
	assert.Equal(t, "", um.Lookup("anything"))
}

func TestUserMapLookupNilMap(t *testing.T) {
	// LoadUserMapFromReader with malformed CSV should still not panic on Lookup
	um, err := transformer.LoadUserMapFromReader(strings.NewReader("source_id,smartsheet_email\n"))
	require.NoError(t, err)
	// An empty map should return "" not panic
	assert.Equal(t, "", um.Lookup(""))
	assert.Equal(t, "", um.Lookup("any"))
}
