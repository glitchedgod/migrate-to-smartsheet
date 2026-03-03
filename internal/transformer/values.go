package transformer

import (
	"strings"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

// NormalizeDate converts any date string to YYYY-MM-DD.
// Returns "" for nil or empty input.
func NormalizeDate(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	if len(s) == 10 {
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return s
		}
		return "" // 10-char string that is not a valid YYYY-MM-DD
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return s
}

// FormatContact converts an email string to a Smartsheet CONTACT_LIST cell value.
// Smartsheet expects {"email": "..."} objects. Returns nil for empty email.
func FormatContact(email string) interface{} {
	if email == "" {
		return nil
	}
	return map[string]string{"email": email}
}

// FormatMultiSelect converts a []string to a comma-separated string for MULTI_PICKLIST.
// Any commas within individual values are replaced with spaces to avoid breaking the delimiter.
func FormatMultiSelect(values []string) string {
	if len(values) == 0 {
		return ""
	}
	sanitized := make([]string, len(values))
	for i, v := range values {
		sanitized[i] = strings.ReplaceAll(v, ",", " ")
	}
	return strings.Join(sanitized, ",")
}

// FormatBool converts various bool representations to an actual bool.
func FormatBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	}
	return false
}

// TransformCellValue applies the correct value transform for a given column type.
// It also applies user remapping for Contact/MultiContact columns.
func TransformCellValue(v interface{}, colType model.ColumnType, um *UserMap) interface{} {
	if v == nil {
		return nil
	}
	switch colType {
	case model.TypeDate, model.TypeDateTime:
		return NormalizeDate(v)

	case model.TypeContact:
		email := extractEmail(v, um)
		if email == "" {
			return nil
		}
		return FormatContact(email)

	case model.TypeMultiContact:
		switch val := v.(type) {
		case []string:
			contacts := make([]map[string]string, 0, len(val))
			for _, e := range val {
				if mapped := um.Lookup(e); mapped != "" {
					e = mapped
				}
				if e != "" {
					contacts = append(contacts, map[string]string{"email": e})
				}
			}
			if len(contacts) == 0 {
				return nil
			}
			return contacts
		case string:
			if mapped := um.Lookup(val); mapped != "" {
				val = mapped
			}
			return FormatContact(val)
		}
		return v

	case model.TypeMultiSelect:
		switch val := v.(type) {
		case []string:
			return FormatMultiSelect(val)
		case []interface{}:
			strs := make([]string, 0, len(val))
			for _, item := range val {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			return FormatMultiSelect(strs)
		}
		return v

	case model.TypeCheckbox:
		return FormatBool(v)
	}

	return v
}

func extractEmail(v interface{}, um *UserMap) string {
	var email string
	switch val := v.(type) {
	case string:
		email = val
	case map[string]string:
		email = val["email"]
	case map[string]interface{}:
		if e, ok := val["email"].(string); ok {
			email = e
		}
	}
	if email == "" {
		return ""
	}
	if mapped := um.Lookup(email); mapped != "" {
		return mapped
	}
	return email
}
