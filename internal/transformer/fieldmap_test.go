package transformer_test

import (
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestToSmartsheetColumnType(t *testing.T) {
	cases := []struct {
		in  model.ColumnType
		out string
	}{
		{model.TypeText, "TEXT_NUMBER"},
		{model.TypeNumber, "TEXT_NUMBER"},
		{model.TypeDate, "DATE"},
		{model.TypeDateTime, "DATETIME"},
		{model.TypeCheckbox, "CHECKBOX"},
		{model.TypeSingleSelect, "PICKLIST"},
		{model.TypeMultiSelect, "MULTI_PICKLIST"},
		{model.TypeContact, "CONTACT_LIST"},
		{model.TypeMultiContact, "MULTI_CONTACT_LIST"},
		{model.TypeURL, "TEXT_NUMBER"},
		{model.TypeDuration, "TEXT_NUMBER"},
	}
	for _, c := range cases {
		assert.Equal(t, c.out, transformer.ToSmartsheetColumnType(c.in), "input: %s", c.in)
	}
}
