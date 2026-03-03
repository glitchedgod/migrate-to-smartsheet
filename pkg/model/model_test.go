package model_test

import (
	"testing"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestWorkspaceHierarchy(t *testing.T) {
	ws := model.Workspace{
		ID:   "ws1",
		Name: "My Workspace",
		Projects: []model.Project{
			{
				ID:   "proj1",
				Name: "Alpha",
				Columns: []model.ColumnDef{
					{Name: "Title", Type: model.TypeText},
					{Name: "Due Date", Type: model.TypeDate},
					{Name: "Status", Type: model.TypeSingleSelect, Options: []string{"Todo", "Done"}},
				},
				Rows: []model.Row{
					{
						ID: "row1",
						Cells: []model.Cell{
							{ColumnName: "Title", Value: "First task"},
							{ColumnName: "Due Date", Value: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
							{ColumnName: "Status", Value: "Todo"},
						},
					},
				},
			},
		},
	}

	assert.Equal(t, "ws1", ws.ID)
	assert.Len(t, ws.Projects, 1)
	assert.Len(t, ws.Projects[0].Rows, 1)
	assert.Equal(t, model.TypeDate, ws.Projects[0].Columns[1].Type)
}
