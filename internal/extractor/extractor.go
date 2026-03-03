package extractor

import (
	"context"
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
