package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

type Extractor struct {
	email   string
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(email, token, instanceURL string, opts ...Option) *Extractor {
	e := &Extractor{
		email:   email,
		token:   token,
		baseURL: instanceURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("jira"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return err
	}
	creds := base64.StdEncoding.EncodeToString([]byte(e.email + ":" + e.token))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("jira GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	return []model.Workspace{{ID: e.baseURL, Name: e.baseURL}}, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectKey string, opts extractor.Options) (*model.Project, error) {
	var allIssues []struct {
		ID     string                 `json:"id"`
		Key    string                 `json:"key"`
		Fields map[string]interface{} `json:"fields"`
	}

	startAt := 0
	for {
		var resp struct {
			Issues []struct {
				ID     string                 `json:"id"`
				Key    string                 `json:"key"`
				Fields map[string]interface{} `json:"fields"`
			} `json:"issues"`
			Total int `json:"total"`
		}
		path := fmt.Sprintf("/rest/api/3/search?jql=project=%s&startAt=%d&maxResults=100", projectKey, startAt)
		if err := e.get(ctx, path, &resp); err != nil {
			return nil, err
		}
		allIssues = append(allIssues, resp.Issues...)
		if startAt+len(resp.Issues) >= resp.Total {
			break
		}
		startAt += len(resp.Issues)
	}

	columns := []model.ColumnDef{
		{Name: "Summary", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "Status", Type: model.TypeSingleSelect},
		{Name: "Assignee", Type: model.TypeContact},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Key", Type: model.TypeText},
	}

	rows := make([]model.Row, 0, len(allIssues))
	for _, issue := range allIssues {
		// Summary first so Cells[0] is the summary value.
		var cells []model.Cell
		if v, ok := issue.Fields["summary"].(string); ok {
			cells = append(cells, model.Cell{ColumnName: "Summary", Value: v})
		}
		if desc := issue.Fields["description"]; desc != nil {
			cells = append(cells, model.Cell{ColumnName: "Description", Value: transformer.ADFToPlainText(desc)})
		}
		if status, ok := issue.Fields["status"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Status", Value: status["name"]})
		}
		if assignee, ok := issue.Fields["assignee"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Assignee", Value: assignee["emailAddress"]})
		}
		if due, ok := issue.Fields["duedate"].(string); ok {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: due})
		}
		cells = append(cells, model.Cell{ColumnName: "Key", Value: issue.Key})
		rows = append(rows, model.Row{ID: issue.ID, Cells: cells})
	}

	return &model.Project{ID: projectKey, Name: projectKey, Columns: columns, Rows: rows}, nil
}
