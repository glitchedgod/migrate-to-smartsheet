package transformer

import "github.com/bchauhan/migrate-to-smartsheet/pkg/model"

func ToSmartsheetColumnType(t model.ColumnType) string {
	switch t {
	case model.TypeDate:
		return "DATE"
	case model.TypeDateTime:
		return "DATETIME"
	case model.TypeCheckbox:
		return "CHECKBOX"
	case model.TypeSingleSelect:
		return "PICKLIST"
	case model.TypeMultiSelect:
		return "MULTI_PICKLIST"
	case model.TypeContact:
		return "CONTACT_LIST"
	case model.TypeMultiContact:
		return "MULTI_CONTACT_LIST"
	default:
		return "TEXT_NUMBER"
	}
}
