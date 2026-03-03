package preview

import (
	"fmt"

	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

const maxAttachmentBytes = 25 * 1024 * 1024

type Summary struct {
	Workspaces           int
	Sheets               int
	Rows                 int
	Columns              int
	Attachments          int
	Comments             int
	OversizedAttachments int
	Warnings             []string
}

func Summarize(workspaces []model.Workspace) Summary {
	s := Summary{Workspaces: len(workspaces)}
	for _, ws := range workspaces {
		s.Sheets += len(ws.Projects)
		for _, proj := range ws.Projects {
			s.Columns += len(proj.Columns)
			s.Rows += len(proj.Rows)
			for _, row := range proj.Rows {
				s.Attachments += len(row.Attachments)
				s.Comments += len(row.Comments)
				for _, att := range row.Attachments {
					if att.SizeBytes > maxAttachmentBytes {
						s.OversizedAttachments++
					}
				}
			}
		}
	}
	if s.OversizedAttachments > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%d attachment(s) exceed 25MB and will be skipped", s.OversizedAttachments))
	}
	return s
}
