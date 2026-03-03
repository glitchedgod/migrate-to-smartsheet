package asana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://app.asana.com/api/1.0"

type Extractor struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(token string, opts ...Option) *Extractor {
	e := &Extractor{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("asana"),
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
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("asana GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Data []struct {
			GID  string `json:"gid"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := e.get(ctx, "/workspaces", &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(resp.Data))
	for i, w := range resp.Data {
		ws[i] = model.Workspace{ID: w.GID, Name: w.Name}
	}
	return ws, nil
}

type asanaTask struct {
	GID       string `json:"gid"`
	Name      string `json:"name"`
	Notes     string `json:"notes"`
	Completed bool   `json:"completed"`
	DueOn     string `json:"due_on"`
	Assignee  *struct {
		GID   string `json:"gid"`
		Email string `json:"email"`
	} `json:"assignee"`
	Tags   []struct{ Name string `json:"name"` } `json:"tags"`
	Parent *struct{ GID string `json:"gid"` } `json:"parent"`
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	var resp struct{ Data []asanaTask `json:"data"` }
	path := fmt.Sprintf("/projects/%s/tasks?opt_fields=gid,name,notes,completed,due_on,assignee.email,tags.name,parent.gid", projectID)
	if err := e.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Notes", Type: model.TypeText},
		{Name: "Completed", Type: model.TypeCheckbox},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Assignee", Type: model.TypeContact},
		{Name: "Tags", Type: model.TypeMultiSelect},
	}

	rows := make([]model.Row, 0, len(resp.Data))
	for _, t := range resp.Data {
		if !opts.IncludeArchived && t.Completed {
			continue
		}
		cells := []model.Cell{
			{ColumnName: "Name", Value: t.Name},
			{ColumnName: "Notes", Value: t.Notes},
			{ColumnName: "Completed", Value: t.Completed},
		}
		if t.DueOn != "" {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: t.DueOn})
		}
		if t.Assignee != nil {
			cells = append(cells, model.Cell{ColumnName: "Assignee", Value: t.Assignee.Email})
		}
		if len(t.Tags) > 0 {
			tags := make([]string, len(t.Tags))
			for i, tag := range t.Tags {
				tags[i] = tag.Name
			}
			cells = append(cells, model.Cell{ColumnName: "Tags", Value: tags})
		}
		parentID := ""
		if t.Parent != nil {
			parentID = t.Parent.GID
		}
		rows = append(rows, model.Row{ID: t.GID, ParentID: parentID, Cells: cells})
	}

	return &model.Project{ID: projectID, Name: projectID, Columns: columns, Rows: rows}, nil
}
