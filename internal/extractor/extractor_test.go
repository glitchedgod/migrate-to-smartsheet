package extractor_test

import (
	"context"
	"testing"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
	"github.com/stretchr/testify/assert"
)

type mockExtractor struct{}

func (m *mockExtractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	return []model.Workspace{{ID: "ws1", Name: "Test WS"}}, nil
}
func (m *mockExtractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	return []extractor.ProjectRef{{ID: "proj1", Name: "Test Project"}}, nil
}
func (m *mockExtractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	return &model.Project{ID: projectID, Name: "Test Project"}, nil
}

func TestExtractorInterface(t *testing.T) {
	var e extractor.Extractor = &mockExtractor{}
	workspaces, err := e.ListWorkspaces(context.Background())
	assert.NoError(t, err)
	assert.Len(t, workspaces, 1)
}

func TestListProjectsInterface(t *testing.T) {
	var e extractor.Extractor = &mockExtractor{}
	projects, err := e.ListProjects(context.Background(), "ws1")
	assert.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, "proj1", projects[0].ID)
	assert.Equal(t, "Test Project", projects[0].Name)
}
