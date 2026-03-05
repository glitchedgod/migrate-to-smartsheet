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

func (e *Extractor) get(ctx context.Context, path string, out interface{}) error {
	e.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Notion-Version", notionVersion)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notion GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
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

func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var refs []extractor.ProjectRef
	var cursor *string
	for {
		body := map[string]interface{}{
			"filter":    map[string]interface{}{"value": "database", "property": "object"},
			"page_size": 100,
		}
		if cursor != nil {
			body["start_cursor"] = *cursor
		}
		var resp struct {
			Results []struct {
				ID    string `json:"id"`
				Title []struct {
					PlainText string `json:"plain_text"`
				} `json:"title"`
			} `json:"results"`
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := e.post(ctx, "/search", body, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Results {
			name := r.ID
			if len(r.Title) > 0 {
				name = r.Title[0].PlainText
			}
			refs = append(refs, extractor.ProjectRef{ID: r.ID, Name: name})
		}
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}
	return refs, nil
}

func notionPropTypeToCanonical(propType string) model.ColumnType {
	switch propType {
	case "date", "created_time", "last_edited_time":
		return model.TypeDate
	case "checkbox":
		return model.TypeCheckbox
	case "select", "status":
		return model.TypeSingleSelect
	case "multi_select":
		return model.TypeMultiSelect
	case "people", "created_by", "last_edited_by":
		return model.TypeMultiContact
	case "number":
		return model.TypeNumber
	default:
		return model.TypeText
	}
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, databaseID string, opts extractor.Options) (*model.Project, error) {
	// Fetch database metadata to get the title and typed property schema.
	dbName := databaseID
	propTypes := map[string]string{} // propName → notion type string
	var dbMeta struct {
		Title []struct {
			PlainText string `json:"plain_text"`
		} `json:"title"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := e.get(ctx, fmt.Sprintf("/databases/%s", databaseID), &dbMeta); err == nil {
		if len(dbMeta.Title) > 0 && dbMeta.Title[0].PlainText != "" {
			dbName = dbMeta.Title[0].PlainText
		}
		for name, prop := range dbMeta.Properties {
			propTypes[name] = prop.Type
		}
	}

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

	// Build columns from schema if available, else infer from pages.
	// The title property must be first (it becomes the Smartsheet primary column).
	colSet := map[string]bool{}
	for _, p := range allPages {
		if props, ok := p["properties"].(map[string]interface{}); ok {
			for k := range props {
				colSet[k] = true
			}
		}
	}
	// Find the title property name so we can put it first.
	titleProp := ""
	for name, t := range propTypes {
		if t == "title" {
			titleProp = name
			break
		}
	}
	columns := make([]model.ColumnDef, 0, len(colSet))
	// Add title column first.
	if titleProp != "" && colSet[titleProp] {
		columns = append(columns, model.ColumnDef{Name: titleProp, Type: model.TypeText})
	}
	for name := range colSet {
		if name == titleProp {
			continue
		}
		colType := model.TypeText
		if t, ok := propTypes[name]; ok {
			colType = notionPropTypeToCanonical(t)
		}
		columns = append(columns, model.ColumnDef{Name: name, Type: colType})
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
			value := extractNotionPropValue(propVal)
			if value != nil {
				cells = append(cells, model.Cell{ColumnName: colName, Value: value})
			}
		}
		rows = append(rows, model.Row{ID: id, Cells: cells})
	}

	return &model.Project{ID: databaseID, Name: dbName, Columns: columns, Rows: rows}, nil
}

func extractNotionPropValue(prop interface{}) interface{} {
	m, ok := prop.(map[string]interface{})
	if !ok {
		return nil
	}

	// title
	if title, ok := m["title"].([]interface{}); ok && len(title) > 0 {
		if rt, ok := title[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rt["plain_text"])
		}
	}
	// rich_text
	if rt, ok := m["rich_text"].([]interface{}); ok && len(rt) > 0 {
		if rtm, ok := rt[0].(map[string]interface{}); ok {
			return fmt.Sprintf("%v", rtm["plain_text"])
		}
	}
	// select
	if sel, ok := m["select"].(map[string]interface{}); ok && sel != nil {
		return fmt.Sprintf("%v", sel["name"])
	}
	// status
	if status, ok := m["status"].(map[string]interface{}); ok && status != nil {
		return fmt.Sprintf("%v", status["name"])
	}
	// multi_select → []string
	if ms, ok := m["multi_select"].([]interface{}); ok {
		vals := make([]string, 0, len(ms))
		for _, v := range ms {
			if vm, ok := v.(map[string]interface{}); ok {
				vals = append(vals, fmt.Sprintf("%v", vm["name"]))
			}
		}
		if len(vals) > 0 {
			return vals
		}
		return nil
	}
	// date
	if date, ok := m["date"].(map[string]interface{}); ok && date != nil {
		start := fmt.Sprintf("%v", date["start"])
		if end, ok := date["end"].(string); ok && end != "" {
			return start + " – " + end
		}
		return start
	}
	// checkbox
	if cb, ok := m["checkbox"]; ok {
		return cb
	}
	// number
	if num, ok := m["number"]; ok && num != nil {
		return num
	}
	// url
	if url, ok := m["url"].(string); ok && url != "" {
		return url
	}
	// email
	if email, ok := m["email"].(string); ok && email != "" {
		return email
	}
	// phone_number
	if phone, ok := m["phone_number"].(string); ok && phone != "" {
		return phone
	}
	// people → []string (emails)
	if people, ok := m["people"].([]interface{}); ok && len(people) > 0 {
		emails := make([]string, 0, len(people))
		for _, p := range people {
			if pm, ok := p.(map[string]interface{}); ok {
				if person, ok := pm["person"].(map[string]interface{}); ok {
					if email, ok := person["email"].(string); ok {
						emails = append(emails, email)
					}
				}
			}
		}
		if len(emails) > 0 {
			return emails
		}
	}
	// created_time
	if ct, ok := m["created_time"].(string); ok && ct != "" {
		return ct
	}
	// last_edited_time
	if let, ok := m["last_edited_time"].(string); ok && let != "" {
		return let
	}
	// formula
	if formula, ok := m["formula"].(map[string]interface{}); ok {
		for _, key := range []string{"string", "number", "boolean"} {
			if v, ok := formula[key]; ok && v != nil {
				return fmt.Sprintf("%v", v)
			}
		}
		if d, ok := formula["date"].(map[string]interface{}); ok && d != nil {
			return fmt.Sprintf("%v", d["start"])
		}
	}
	// unique_id
	if uid, ok := m["unique_id"].(map[string]interface{}); ok {
		prefix, _ := uid["prefix"].(string)
		num := uid["number"]
		if prefix != "" {
			return fmt.Sprintf("%s-%v", prefix, num)
		}
		return fmt.Sprintf("%v", num)
	}

	return nil
}
