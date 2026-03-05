package trello

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
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
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	url := fmt.Sprintf("%s%s%skey=%s&token=%s", e.baseURL, path, sep, e.key, e.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
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

func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var boards []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := e.get(ctx, fmt.Sprintf("/organizations/%s/boards?fields=id,name", workspaceID), &boards); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(boards))
	for i, b := range boards {
		refs[i] = extractor.ProjectRef{ID: b.ID, Name: b.Name}
	}
	return refs, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, projectID string, opts extractor.Options) (*model.Project, error) {
	// Fetch board name
	var board struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	boardName := projectID
	if err := e.get(ctx, fmt.Sprintf("/boards/%s?fields=id,name", projectID), &board); err == nil && board.Name != "" {
		boardName = board.Name
	}

	// Fetch lists for name resolution
	var lists []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = e.get(ctx, fmt.Sprintf("/boards/%s/lists?fields=id,name", projectID), &lists)
	listNameByID := make(map[string]string, len(lists))
	listNames := make([]string, 0, len(lists))
	for _, l := range lists {
		listNameByID[l.ID] = l.Name
		listNames = append(listNames, l.Name)
	}

	// Fetch cards with pagination (Trello max 1000 per request; paginate via `before`)
	type trelloCard struct {
		ID     string  `json:"id"`
		Name   string  `json:"name"`
		Desc   string  `json:"desc"`
		Closed bool    `json:"closed"`
		Due    *string `json:"due"`
		IDList string  `json:"idList"`
		Labels []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"labels"`
	}
	var cards []trelloCard
	before := ""
	for {
		path := fmt.Sprintf("/boards/%s/cards?limit=1000&fields=id,name,desc,closed,due,idList,labels", projectID)
		if before != "" {
			path += "&before=" + before
		}
		var page []trelloCard
		if err := e.get(ctx, path, &page); err != nil {
			return nil, err
		}
		cards = append(cards, page...)
		if len(page) < 1000 {
			break
		}
		before = page[len(page)-1].ID
	}

	columns := []model.ColumnDef{
		{Name: "Name", Type: model.TypeText},
		{Name: "Description", Type: model.TypeText},
		{Name: "List", Type: model.TypeSingleSelect, Options: listNames},
		{Name: "Due Date", Type: model.TypeDate},
		{Name: "Closed", Type: model.TypeCheckbox},
		{Name: "Labels", Type: model.TypeMultiSelect},
	}

	rows := make([]model.Row, 0, len(cards))
	for _, c := range cards {
		if !opts.IncludeArchived && c.Closed {
			continue
		}
		listName := listNameByID[c.IDList]
		if listName == "" {
			listName = c.IDList
		}
		cells := []model.Cell{
			{ColumnName: "Name", Value: c.Name},
			{ColumnName: "Description", Value: c.Desc},
			{ColumnName: "List", Value: listName},
			{ColumnName: "Closed", Value: c.Closed},
		}
		if c.Due != nil {
			cells = append(cells, model.Cell{ColumnName: "Due Date", Value: *c.Due})
		}
		if len(c.Labels) > 0 {
			labels := make([]string, 0, len(c.Labels))
			for _, l := range c.Labels {
				if l.Name != "" {
					labels = append(labels, l.Name)
				} else if l.Color != "" {
					labels = append(labels, l.Color)
				}
			}
			if len(labels) > 0 {
				cells = append(cells, model.Cell{ColumnName: "Labels", Value: labels})
			}
		}
		rows = append(rows, model.Row{ID: c.ID, Cells: cells})
	}

	return &model.Project{ID: projectID, Name: boardName, Columns: columns, Rows: rows}, nil
}
