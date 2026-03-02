package trello

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.trello.com/1"

type Extractor struct {
	key     string
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

func New(key, token string, opts ...Option) *Extractor {
	e := &Extractor{
		key: key, token: token,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		rl:      ratelimit.ForPlatform("trello"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	url := fmt.Sprintf("%s%s?key=%s&token=%s", e.baseURL, path, e.key, e.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("trello GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var orgs []struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}
	if err := e.get(ctx, "/members/me/organizations", &orgs); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(orgs))
	for i, o := range orgs {
		ws[i] = model.Workspace{ID: o.ID, Name: o.DisplayName}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	var cards []struct {
		ID     string  `json:"id"`
		Name   string  `json:"name"`
		Desc   string  `json:"desc"`
		Closed bool    `json:"closed"`
		Due    *string `json:"due"`
		IDList string  `json:"idList"`
	}
	if err := e.get(ctx, fmt.Sprintf("/boards/%s/cards", projectID), &cards); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "List", Type: model.TypeSingleSelect},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Closed", Type: model.TypeCheckbox},
	}

	rows := make([]model.Row, 0, len(cards))
	for _, c := range cards {
		if !opts.IncludeArchived && c.Closed {
			continue
		}
		cells := []model.Cell{
			{ColumnName: "Name", Value: c.Name},
			{ColumnName: "Description", Value: c.Desc},
			{ColumnName: "List", Value: c.IDList},
			{ColumnName: "Closed", Value: c.Closed},
		}
		if c.Due != nil {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: *c.Due})
		}
		rows = append(rows, model.Row{ID: c.ID, Cells: cells})
	}

	return &model.Project{ID: projectID, Name: projectID, Columns: columns, Rows: rows}, nil
}
