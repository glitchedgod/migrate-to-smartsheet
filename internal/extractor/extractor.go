package extractor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

// ProjectRef is a lightweight project identifier returned by ListProjects.
type ProjectRef struct {
	ID   string
	Name string
}

// Options configure what an extractor fetches.
type Options struct {
	IncludeAttachments bool
	IncludeComments    bool
	IncludeArchived    bool
	CreatedAfter       time.Time
	UpdatedAfter       time.Time
	ExcludeFields      []string
}

// Extractor is implemented by each source platform adapter.
type Extractor interface {
	ListWorkspaces(ctx context.Context) ([]model.Workspace, error)
	ListProjects(ctx context.Context, workspaceID string) ([]ProjectRef, error)
	ExtractProject(ctx context.Context, workspaceID, projectID string, opts Options) (*model.Project, error)
}

// PopulateSelectOptions scans all rows in proj and fills in Options for any
// TypeSingleSelect or TypeMultiSelect columns that have no options yet.
// This ensures Smartsheet PICKLIST columns are created with the correct choices.
func PopulateSelectOptions(proj *model.Project) {
	// Build a map of column name → index for select-type columns without options.
	selectCols := map[string]int{}
	for i, c := range proj.Columns {
		if (c.Type == model.TypeSingleSelect || c.Type == model.TypeMultiSelect) && len(c.Options) == 0 {
			selectCols[c.Name] = i
		}
	}
	if len(selectCols) == 0 {
		return
	}

	// Collect unique values per column.
	seen := make(map[string]map[string]bool, len(selectCols))
	order := make(map[string][]string, len(selectCols))
	for name := range selectCols {
		seen[name] = map[string]bool{}
	}

	for _, row := range proj.Rows {
		for _, cell := range row.Cells {
			idx, ok := selectCols[cell.ColumnName]
			if !ok {
				continue
			}
			colType := proj.Columns[idx].Type
			switch v := cell.Value.(type) {
			case string:
				if v != "" && !seen[cell.ColumnName][v] {
					seen[cell.ColumnName][v] = true
					order[cell.ColumnName] = append(order[cell.ColumnName], v)
				}
			case []string:
				for _, s := range v {
					if s != "" && !seen[cell.ColumnName][s] {
						seen[cell.ColumnName][s] = true
						order[cell.ColumnName] = append(order[cell.ColumnName], s)
					}
				}
			case []interface{}:
				if colType == model.TypeMultiSelect {
					for _, item := range v {
						s := strings.TrimSpace(fmt.Sprintf("%v", item))
						if s != "" && !seen[cell.ColumnName][s] {
							seen[cell.ColumnName][s] = true
							order[cell.ColumnName] = append(order[cell.ColumnName], s)
						}
					}
				}
			}
		}
	}

	for name, idx := range selectCols {
		proj.Columns[idx].Options = order[name]
	}
}
