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

type airtableField struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Options *struct {
		Choices []struct{ Name string `json:"name"` } `json:"choices"`
	} `json:"options"`
}

type airtableTable struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Fields []airtableField `json:"fields"`
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

func (e *Extractor) ListProjects(ctx context.Context, baseID string) ([]extractor.ProjectRef, error) {
	var resp struct {
		Tables []airtableTable `json:"tables"`
	}
	if err := e.get(ctx, fmt.Sprintf("%s/meta/bases/%s/tables", e.baseURL, baseID), &resp); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(resp.Tables))
	for i, t := range resp.Tables {
		refs[i] = extractor.ProjectRef{ID: t.ID, Name: t.Name}
	}
	return refs, nil
}

func airtableTypeToCanonical(at string) model.ColumnType {
	switch at {
	case "date":
		return model.TypeDate
	case "dateTime":
		return model.TypeDateTime
	case "checkbox":
		return model.TypeCheckbox
	case "singleSelect":
		return model.TypeSingleSelect
	case "multipleSelects":
		return model.TypeMultiSelect
	case "singleCollaborator", "createdBy", "lastModifiedBy":
		return model.TypeContact
	case "multipleCollaborators":
		return model.TypeMultiContact
	case "number", "currency", "percent", "duration":
		return model.TypeNumber
	default:
		return model.TypeText
	}
}

func extractAirtableValue(v interface{}, fieldType string) interface{} {
	if v == nil {
		return nil
	}
	switch fieldType {
	case "aiText", "formula", "rollup", "lookup", "count":
		// Computed/AI fields return structured objects — skip them to avoid
		// raw Go map representation appearing as cell values.
		return nil
	case "multipleSelects":
		if arr, ok := v.([]interface{}); ok {
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			return strs
		}
	case "singleCollaborator", "createdBy", "lastModifiedBy":
		if m, ok := v.(map[string]interface{}); ok {
			if email, ok := m["email"].(string); ok {
				return email
			}
		}
	case "multipleCollaborators":
		if arr, ok := v.([]interface{}); ok {
			emails := make([]string, 0)
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if email, ok := m["email"].(string); ok {
						emails = append(emails, email)
					}
				}
			}
			return emails
		}
	case "multipleAttachments":
		// Return nil — attachment objects are handled separately in ExtractProject
		// by populating model.Row.Attachments, not as cell values.
		return nil
	}
	return fmt.Sprintf("%v", v)
}

func (e *Extractor) ExtractProject(ctx context.Context, baseID, tableID string, opts extractor.Options) (*model.Project, error) {
	// Fetch schema for typed columns
	fieldsByName := make(map[string]airtableField)
	tableName := tableID
	var schemaResp struct {
		Tables []airtableTable `json:"tables"`
	}
	if err := e.get(ctx, fmt.Sprintf("%s/meta/bases/%s/tables", e.baseURL, baseID), &schemaResp); err == nil {
		for _, t := range schemaResp.Tables {
			if t.ID == tableID || t.Name == tableID {
				tableName = t.Name
				for _, f := range t.Fields {
					fieldsByName[f.Name] = f
				}
				break
			}
		}
	}

	// Fetch records with pagination
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

	// Build typed columns — from schema if available, else infer from records
	var colOrder []string
	colDefs := make(map[string]model.ColumnDef)
	if len(fieldsByName) > 0 {
		for name, f := range fieldsByName {
			choices := make([]string, 0)
			if f.Options != nil {
				for _, c := range f.Options.Choices {
					choices = append(choices, c.Name)
				}
			}
			colDefs[name] = model.ColumnDef{Name: name, Type: airtableTypeToCanonical(f.Type), Options: choices}
			colOrder = append(colOrder, name)
		}
	} else {
		seen := map[string]bool{}
		for _, r := range allRecords {
			for k := range r.Fields {
				if !seen[k] {
					seen[k] = true
					colDefs[k] = model.ColumnDef{Name: k, Type: model.TypeText}
					colOrder = append(colOrder, k)
				}
			}
		}
	}
	columns := make([]model.ColumnDef, 0, len(colOrder))
	for _, name := range colOrder {
		columns = append(columns, colDefs[name])
	}

	rows := make([]model.Row, 0, len(allRecords))
	for _, r := range allRecords {
		cells := make([]model.Cell, 0, len(r.Fields))
		var attachments []model.Attachment
		for name, v := range r.Fields {
			fieldType := ""
			if f, ok := fieldsByName[name]; ok {
				fieldType = f.Type
			}
			if fieldType == "multipleAttachments" {
				if arr, ok := v.([]interface{}); ok {
					for _, item := range arr {
						if m, ok := item.(map[string]interface{}); ok {
							url, _ := m["url"].(string)
							filename, _ := m["filename"].(string)
							var size int64
							if sz, ok := m["size"].(float64); ok {
								size = int64(sz)
							}
							if url != "" {
								attachments = append(attachments, model.Attachment{
									URL:       url,
									Name:      filename,
									SizeBytes: size,
								})
							}
						}
					}
				}
				continue // do not add a cell for attachment fields
			}
			val := extractAirtableValue(v, fieldType)
			if val != nil {
				cells = append(cells, model.Cell{ColumnName: name, Value: val})
			}
		}
		rows = append(rows, model.Row{ID: r.ID, Cells: cells, Attachments: attachments})
	}

	proj := &model.Project{ID: tableID, Name: tableName, Columns: columns, Rows: rows}
	extractor.PopulateSelectOptions(proj)
	return proj, nil
}
