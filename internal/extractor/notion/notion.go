package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.notion.com/v1"
const notionVersion = "2022-06-28"

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
		rl:      ratelimit.ForPlatform("notion"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) post(ctx context.Context, path string, body interface{}, out interface{}) error {
	e.rl.Wait()
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notion POST %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var resp struct {
		Results []struct {
			ID    string `json:"id"`
			Title []struct {
				PlainText string `json:"plain_text"`
			} `json:"title"`
		} `json:"results"`
	}
	body := map[string]interface{}{"filter": map[string]interface{}{"value": "database", "property": "object"}}
	if err := e.post(ctx, "/search", body, &resp); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, 0, len(resp.Results))
	for _, r := range resp.Results {
		name := r.ID
		if len(r.Title) > 0 {
			name = r.Title[0].PlainText
		}
		ws = append(ws, model.Workspace{ID: r.ID, Name: name})
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, databaseID string, opts extractor.Options) (*model.Project, error) {
	var allPages []map[string]interface{}
	var cursor *string

	for {
		body := map[string]interface{}{"page_size": 100}
		if cursor != nil {
			body["start_cursor"] = *cursor
		}
		var resp struct {
			Results    []map[string]interface{} `json:"results"`
			HasMore    bool                     `json:"has_more"`
			NextCursor *string                  `json:"next_cursor"`
		}
		if err := e.post(ctx, fmt.Sprintf("/databases/%s/query", databaseID), body, &resp); err != nil {
			return nil, err
		}
		allPages = append(allPages, resp.Results...)
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}

	colSet := map[string]bool{}
	for _, p := range allPages {
		if props, ok := p["properties"].(map[string]interface{}); ok {
			for k := range props {
				colSet[k] = true
			}
		}
	}
	columns := make([]model.ColumnDef, 0, len(colSet))
	for name := range colSet {
		columns = append(columns, model.ColumnDef{Name: name, Type: model.TypeText})
	}

	rows := make([]model.Row, 0, len(allPages))
	for _, p := range allPages {
		id, _ := p["id"].(string)
		props, ok := p["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		cells := make([]model.Cell, 0)
		for colName, propVal := range props {
			if value := extractNotionPropValue(propVal); value != "" {
				cells = append(cells, model.Cell{ColumnName: colName, Value: value})
			}
		}
		rows = append(rows, model.Row{ID: id, Cells: cells})
	}

	return &model.Project{ID: databaseID, Name: databaseID, Columns: columns, Rows: rows}, nil
}

func extractNotionPropValue(prop interface{}) string {
	m, ok := prop.(map[string]interface{})
	if !ok {
		return ""
	}
	if title, ok := m["title"].([]interface{}); ok && len(title) > 0 {
		if rt, ok := title[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rt["plain_text"])
		}
	}
	if sel, ok := m["select"].(map[string]interface{}); ok {
		return fmt.Sprintf("%v", sel["name"])
	}
	if date, ok := m["date"].(map[string]interface{}); ok {
		return fmt.Sprintf("%v", date["start"])
	}
	if cb, ok := m["checkbox"]; ok {
		return fmt.Sprintf("%v", cb)
	}
	if num, ok := m["number"]; ok && num != nil {
		return fmt.Sprintf("%v", num)
	}
	if rt, ok := m["rich_text"].([]interface{}); ok && len(rt) > 0 {
		if rtm, ok := rt[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rtm["plain_text"])
		}
	}
	return ""
}
