package wrike

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://www.wrike.com/api/v4"

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
		rl:      ratelimit.ForPlatform("wrike"),
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
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("wrike GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Data []struct {
			AccountID string `json:"accountId"`
			Profiles  []struct {
				AccountID string `json:"accountId"`
				Name      string `json:"name"`
			} `json:"profiles"`
		} `json:"data"`
	}
	if err := e.get(ctx, "/contacts?me=true", &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("wrike: could not determine account from contacts")
	}
	contact := resp.Data[0]
	accountID := contact.AccountID
	accountName := "Wrike Account"
	for _, p := range contact.Profiles {
		if p.AccountID == accountID {
			accountName = p.Name
			break
		}
	}
	return []model.Workspace{{ID: accountID, Name: accountName}}, nil
}

func (e *Extractor) resolveContactEmails(ctx context.Context, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	// Build comma-separated id list for the query param
	joined := ""
	for i, id := range ids {
		if i > 0 {
			joined += ","
		}
		joined += id
	}
	var resp struct {
		Data []struct {
			ID       string `json:"id"`
			Profiles []struct {
				Email string `json:"email"`
			} `json:"profiles"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/contacts?ids=[%s]", joined)
	if err := e.get(ctx, path, &resp); err != nil {
		return nil
	}
	m := make(map[string]string, len(resp.Data))
	for _, c := range resp.Data {
		if len(c.Profiles) > 0 && c.Profiles[0].Email != "" {
			m[c.ID] = c.Profiles[0].Email
		}
	}
	return m
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, folderID string, opts extractor.Options) (*model.Project, error) {
	// Fetch folder name
	var folderResp struct {
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	folderName := folderID
	if err := e.get(ctx, fmt.Sprintf("/folders/%s", folderID), &folderResp); err == nil && len(folderResp.Data) > 0 {
		folderName = folderResp.Data[0].Title
	}

	var resp struct {
		Data []struct {
			ID           string   `json:"id"`
			Title        string   `json:"title"`
			Description  string   `json:"description"`
			Status       string   `json:"status"`
			ParentIDs    []string `json:"parentIds"`
			Responsibles []string `json:"responsibles"`
			Dates        struct {
				Due string `json:"due"`
			} `json:"dates"`
		} `json:"data"`
	}
	// Only request "description" as an optional field — dates, parentIds, status, and
	// title are returned by default. "responsibles" is not available via this endpoint.
	if err := e.get(ctx, fmt.Sprintf("/folders/%s/tasks?fields=%%5B%%22description%%22%%5D", folderID), &resp); err != nil {
		return nil, err
	}

	columns := []model.ColumnDef{
		{Name: "Title", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "Status", Type: model.TypeSingleSelect},
		{Name: "Due Date", Type: model.TypeDate},
	}

	rows := make([]model.Row, 0, len(resp.Data))
	for _, t := range resp.Data {
		parentID := ""
		if len(t.ParentIDs) > 0 {
			parentID = t.ParentIDs[0]
		}
		cells := []model.Cell{
			{ColumnName: "Title", Value: t.Title},
			{ColumnName: "Description", Value: transformer.StripHTML(t.Description)},
			{ColumnName: "Status", Value: t.Status},
		}
		if t.Dates.Due != "" {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: t.Dates.Due})
		}
		rows = append(rows, model.Row{ID: t.ID, ParentID: parentID, Cells: cells})
	}

	return &model.Project{ID: folderID, Name: folderName, Columns: columns, Rows: rows}, nil
}

// ListProjects lists all project-folders in the given account/workspace.
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Data []struct {
			ID      string                 `json:"id"`
			Title   string                 `json:"title"`
			Project map[string]interface{} `json:"project"`
		} `json:"data"`
	}
	if err := e.get(ctx, "/folders", &resp); err != nil {
		return nil, err
	}
	var refs []extractor.ProjectRef
	for _, f := range resp.Data {
		if f.Project != nil {
			refs = append(refs, extractor.ProjectRef{ID: f.ID, Name: f.Title})
		}
	}
	return refs, nil
}
