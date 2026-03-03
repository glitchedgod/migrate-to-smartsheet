package preview_test

import (
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/preview"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestSummarize(t *testing.T) {
	workspaces := []model.Workspace{
		{
			ID: "ws1",
			Projects: []model.Project{
				{
					Columns: []model.ColumnDef{{Name: "A"}, {Name: "B"}},
					Rows: []model.Row{
						{
							Cells:       []model.Cell{{}, {}},
							Attachments: []model.Attachment{{SizeBytes: 1024}, {SizeBytes: 30 * 1024 * 1024}},
							Comments:    []model.Comment{{}, {}},
						},
						{Cells: []model.Cell{{}, {}}},
					},
				},
			},
		},
	}

	s := preview.Summarize(workspaces)
	assert.Equal(t, 1, s.Workspaces)
	assert.Equal(t, 1, s.Sheets)
	assert.Equal(t, 2, s.Rows)
	assert.Equal(t, 2, s.Columns)
	assert.Equal(t, 2, s.Attachments)
	assert.Equal(t, 2, s.Comments)
	assert.Equal(t, 1, s.OversizedAttachments)
}
