package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("jira GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	return []model.Workspace{{ID: e.baseURL, Name: e.baseURL}}, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectKey string, opts extractor.Options) (*model.Project, error) {
	// Build JQL with optional date filters
	jql := fmt.Sprintf("project=%s ORDER BY created ASC", projectKey)
	if !opts.CreatedAfter.IsZero() && !opts.UpdatedAfter.IsZero() {
		jql = fmt.Sprintf("project=%s AND created >= \"%s\" AND updated >= \"%s\" ORDER BY created ASC", projectKey, opts.CreatedAfter.Format("2006-01-02"), opts.UpdatedAfter.Format("2006-01-02"))
	} else if !opts.CreatedAfter.IsZero() {
		jql = fmt.Sprintf("project=%s AND created >= \"%s\" ORDER BY created ASC", projectKey, opts.CreatedAfter.Format("2006-01-02"))
	} else if !opts.UpdatedAfter.IsZero() {
		jql = fmt.Sprintf("project=%s AND updated >= \"%s\" ORDER BY created ASC", projectKey, opts.UpdatedAfter.Format("2006-01-02"))
	}

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
		path := fmt.Sprintf("/rest/api/3/search/jql?jql=%s&startAt=%d&maxResults=100&fields=summary,description,status,priority,issuetype,assignee,reporter,labels,duedate", url.QueryEscape(jql), startAt)
		if err := e.get(ctx, path, &resp); err != nil {
			return nil, err
		}
		allIssues = append(allIssues, resp.Issues...)
		if len(resp.Issues) == 0 || startAt+len(resp.Issues) >= resp.Total {
			break
		}
		startAt += len(resp.Issues)
	}

	columns := []model.ColumnDef{
		{Name: "Summary", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "Status", Type: model.TypeSingleSelect},
		{Name: "Priority", Type: model.TypeSingleSelect},
		{Name: "Issue Type", Type: model.TypeSingleSelect},
		{Name: "Assignee", Type: model.TypeContact},
		{Name: "Reporter", Type: model.TypeContact},
		{Name: "Labels", Type: model.TypeMultiSelect},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Key", Type: model.TypeText},
	}

	rows := make([]model.Row, 0, len(allIssues))
	for _, issue := range allIssues {
		cells := []model.Cell{{ColumnName: "Key", Value: issue.Key}}

		if v, ok := issue.Fields["summary"].(string); ok {
			cells = append(cells, model.Cell{ColumnName: "Summary", Value: v})
		}
		if desc := issue.Fields["description"]; desc != nil {
			cells = append(cells, model.Cell{ColumnName: "Description", Value: transformer.ADFToPlainText(desc)})
		}
		if status, ok := issue.Fields["status"].(map[string]interface{}); ok {
			cells = append(cells, model.Cell{ColumnName: "Status", Value: status["name"]})
		}
		if priority, ok := issue.Fields["priority"].(map[string]interface{}); ok && priority != nil {
			cells = append(cells, model.Cell{ColumnName: "Priority", Value: priority["name"]})
		}
		if issueType, ok := issue.Fields["issuetype"].(map[string]interface{}); ok && issueType != nil {
			cells = append(cells, model.Cell{ColumnName: "Issue Type", Value: issueType["name"]})
		}
		if assignee, ok := issue.Fields["assignee"].(map[string]interface{}); ok && assignee != nil {
			cells = append(cells, model.Cell{ColumnName: "Assignee", Value: assignee["emailAddress"]})
		}
		if reporter, ok := issue.Fields["reporter"].(map[string]interface{}); ok && reporter != nil {
			cells = append(cells, model.Cell{ColumnName: "Reporter", Value: reporter["emailAddress"]})
		}
		if labels, ok := issue.Fields["labels"].([]interface{}); ok && len(labels) > 0 {
			labelStrs := make([]string, 0, len(labels))
			for _, l := range labels {
				if s, ok := l.(string); ok {
					labelStrs = append(labelStrs, s)
				}
			}
			if len(labelStrs) > 0 {
				cells = append(cells, model.Cell{ColumnName: "Labels", Value: labelStrs})
			}
		}
		if due, ok := issue.Fields["duedate"].(string); ok && due != "" {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: due})
		}

		rows = append(rows, model.Row{ID: issue.ID, Cells: cells})
	}

	proj := &model.Project{ID: projectKey, Name: projectKey, Columns: columns, Rows: rows}
	extractor.PopulateSelectOptions(proj)
	return proj, nil
}

// ListProjects lists all projects accessible to the configured credentials.
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Values []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		}
	}
	startAt := 0
	for {
		path := fmt.Sprintf("/rest/api/3/project/search?maxResults=100&startAt=%d", startAt)
		var page struct {
			Values []struct {
				ID   string `json:"id"`
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"values"`
			Total      int  `json:"total"`
			IsLast     bool `json:"isLast"`
		}
		if err := e.get(ctx, path, &page); err != nil {
			return nil, err
		}
		resp.Values = append(resp.Values, page.Values...)
		if page.IsLast || len(page.Values) == 0 {
			break
		}
		startAt += len(page.Values)
	}
	refs := make([]extractor.ProjectRef, len(resp.Values))
	for i, p := range resp.Values {
		refs[i] = extractor.ProjectRef{ID: p.Key, Name: p.Name}
	}
	return refs, nil
}
