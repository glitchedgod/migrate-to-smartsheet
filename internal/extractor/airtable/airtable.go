package airtable

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

const defaultBaseURL = "https://api.airtable.com/v0"

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
		rl:      ratelimit.ForPlatform("airtable"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) get(ctx context.Context, url string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("airtable GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Bases []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"bases"`
	}
	if err := e.get(ctx, e.baseURL+"/meta/bases", &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(resp.Bases))
	for i, b := range resp.Bases {
		ws[i] = model.Workspace{ID: b.ID, Name: b.Name}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, baseID, tableID string, opts extractor.Options) (*model.Project, error) {
	var allRecords []struct {
		ID     string                 `json:"id"`
		Fields map[string]interface{} `json:"fields"`
	}
	url := fmt.Sprintf("%s/%s/%s?pageSize=100", e.baseURL, baseID, tableID)

	for {
		var resp struct {
			Records []struct {
				ID     string                 `json:"id"`
				Fields map[string]interface{} `json:"fields"`
			} `json:"records"`
			Offset *string `json:"offset"`
		}
		if err := e.get(ctx, url, &resp); err != nil {
			return nil, err
		}
		allRecords = append(allRecords, resp.Records...)
		if resp.Offset == nil {
			break
		}
		url = fmt.Sprintf("%s/%s/%s?pageSize=100&offset=%s", e.baseURL, baseID, tableID, *resp.Offset)
	}

	colSet := map[string]bool{}
	for _, r := range allRecords {
		for k := range r.Fields {
			colSet[k] = true
		}
	}
	columns := make([]model.ColumnDef, 0, len(colSet))
	for name := range colSet {
		columns = append(columns, model.ColumnDef{Name: name, Type: model.TypeText})
	}

	rows := make([]model.Row, 0, len(allRecords))
	for _, r := range allRecords {
		cells := make([]model.Cell, 0, len(r.Fields))
		for k, v := range r.Fields {
			cells = append(cells, model.Cell{ColumnName: k, Value: fmt.Sprintf("%v", v)})
		}
		rows = append(rows, model.Row{ID: r.ID, Cells: cells})
	}

	return &model.Project{ID: tableID, Name: tableID, Columns: columns, Rows: rows}, nil
}

// ListProjects lists all projects in the given workspace.
// TODO: Full implementation coming in a later task.
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	return nil, fmt.Errorf("ListProjects not yet implemented for %T", e)
}
