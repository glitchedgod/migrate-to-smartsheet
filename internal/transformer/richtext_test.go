package transformer_test

import (
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/stretchr/testify/assert"
)

func TestStripHTML(t *testing.T) {
	assert.Equal(t, "Hello world", transformer.StripHTML("<p>Hello <b>world</b></p>"))
	assert.Equal(t, "Line 1 Line 2", transformer.StripHTML("<p>Line 1</p><p>Line 2</p>"))
	assert.Equal(t, "plain text", transformer.StripHTML("plain text"))
	assert.Equal(t, "", transformer.StripHTML(""))
}

func TestADFToPlainText(t *testing.T) {
	adf := map[string]interface{}{
		"type": "doc",
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Hello from Jira",
					},
				},
			},
		},
	}
	assert.Equal(t, "Hello from Jira", transformer.ADFToPlainText(adf))
}

func TestADFToPlainTextNil(t *testing.T) {
	assert.Equal(t, "", transformer.ADFToPlainText(nil))
}

func TestADFToPlainTextHeadings(t *testing.T) {
	adf := map[string]interface{}{
		"type": "doc",
		"content": []interface{}{
			map[string]interface{}{
				"type": "heading",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "My Heading"},
				},
			},
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "Body text"},
				},
			},
		},
	}
	result := transformer.ADFToPlainText(adf)
	assert.Contains(t, result, "My Heading")
	assert.Contains(t, result, "Body text")
}

func TestStripHTMLNestedTags(t *testing.T) {
	assert.Equal(t, "one two three", transformer.StripHTML("<div><p>one</p><ul><li>two</li><li>three</li></ul></div>"))
}

func TestADFToPlainTextWrongType(t *testing.T) {
	assert.Equal(t, "", transformer.ADFToPlainText("not a map"))
	assert.Equal(t, "", transformer.ADFToPlainText(42))
}
