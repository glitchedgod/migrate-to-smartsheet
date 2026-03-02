package extractor

import (
	"context"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

type Options struct {
	IncludeAttachments bool
	IncludeComments    bool
	IncludeArchived    bool
	CreatedAfter       time.Time
	UpdatedAfter       time.Time
	ExcludeFields      []string
}

type Extractor interface {
	ListWorkspaces(ctx context.Context) ([]model.Workspace, error)
	ExtractProject(ctx context.Context, workspaceID, projectID string, opts Options) (*model.Project, error)
}
