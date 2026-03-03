package transformer_test

import (
	"strings"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeDate(t *testing.T) {
	assert.Equal(t, "2026-04-01", transformer.NormalizeDate("2026-04-01"))
	assert.Equal(t, "2026-04-01", transformer.NormalizeDate("2026-04-01T00:00:00.000Z"))
	assert.Equal(t, "2026-04-01", transformer.NormalizeDate("2026-04-01T14:30:00Z"))
	assert.Equal(t, "", transformer.NormalizeDate(""))
	assert.Equal(t, "", transformer.NormalizeDate(nil))
}

func TestFormatContact(t *testing.T) {
	result := transformer.FormatContact("alice@example.com")
	m, ok := result.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "alice@example.com", m["email"])
}

func TestFormatContactEmpty(t *testing.T) {
	assert.Nil(t, transformer.FormatContact(""))
}

func TestFormatMultiSelect(t *testing.T) {
	assert.Equal(t, "a,b,c", transformer.FormatMultiSelect([]string{"a", "b", "c"}))
	assert.Equal(t, "", transformer.FormatMultiSelect([]string{}))
	assert.Equal(t, "", transformer.FormatMultiSelect(nil))
}

func TestFormatBool(t *testing.T) {
	assert.Equal(t, true, transformer.FormatBool(true))
	assert.Equal(t, true, transformer.FormatBool("true"))
	assert.Equal(t, false, transformer.FormatBool("false"))
	assert.Equal(t, false, transformer.FormatBool(false))
	assert.Equal(t, false, transformer.FormatBool(nil))
}

func TestTransformCellValue(t *testing.T) {
	um := transformer.NewUserMap()

	// Date normalization
	v := transformer.TransformCellValue("2026-04-01T00:00:00.000Z", model.TypeDate, um)
	assert.Equal(t, "2026-04-01", v)

	// Contact formatting
	v = transformer.TransformCellValue("dev@example.com", model.TypeContact, um)
	m, ok := v.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "dev@example.com", m["email"])

	// MultiSelect from []string
	v = transformer.TransformCellValue([]string{"a", "b"}, model.TypeMultiSelect, um)
	assert.Equal(t, "a,b", v)

	// Checkbox from string
	v = transformer.TransformCellValue("true", model.TypeCheckbox, um)
	assert.Equal(t, true, v)

	// Text passthrough
	v = transformer.TransformCellValue("hello", model.TypeText, um)
	assert.Equal(t, "hello", v)
}

func TestTransformCellValueUserRemap(t *testing.T) {
	csvData := "source_id,smartsheet_email\nuser1,mapped@example.com\n"
	um, err := transformer.LoadUserMapFromReader(strings.NewReader(csvData))
	assert.NoError(t, err)

	v := transformer.TransformCellValue("user1", model.TypeContact, um)
	m, ok := v.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "mapped@example.com", m["email"])
}

func TestNormalizeDateInvalidTenChar(t *testing.T) {
	// Non-ISO 10-char strings should not pass through as-is
	result := transformer.NormalizeDate("2026/04/01")
	assert.NotEqual(t, "2026/04/01", result, "slash-separated date should not pass through")
	result2 := transformer.NormalizeDate("not-a-dat")
	assert.Equal(t, "not-a-dat", result2, "truly unparseable string falls through")
}

func TestNormalizeDateTimezoneOffset(t *testing.T) {
	// Jira/Wrike may emit timezone-offset timestamps
	result := transformer.NormalizeDate("2026-04-01T10:00:00+05:30")
	assert.Equal(t, "2026-04-01", result)
}

func TestTransformCellValueContactMapInterface(t *testing.T) {
	um := transformer.NewUserMap()
	// JSON-decoded contact (map[string]interface{}) should extract email
	v := transformer.TransformCellValue(
		map[string]interface{}{"email": "alice@example.com", "name": "Alice"},
		model.TypeContact, um,
	)
	m, ok := v.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "alice@example.com", m["email"])
}
