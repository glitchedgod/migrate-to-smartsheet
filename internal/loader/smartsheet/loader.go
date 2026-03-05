package smartsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

// contactCell builds a cellPayload using objectValue for a CONTACT_LIST column.
// The value may be a single email string, a []string of emails, or a
// map[string]interface{} with an "email" key.
func contactCell(colID int64, v interface{}) cellPayload {
	switch val := v.(type) {
	case string:
		if val != "" {
			return cellPayload{ColumnID: colID, ObjectValue: contactObject{ObjectType: "CONTACT", Email: val}}
		}
	case []string:
		if len(val) > 0 {
			// Smartsheet CONTACT_LIST accepts a single contact objectValue.
			// For multiple contacts, use the first email.
			return cellPayload{ColumnID: colID, ObjectValue: contactObject{ObjectType: "CONTACT", Email: val[0]}}
		}
	case []interface{}:
		for _, item := range val {
			if m, ok := item.(map[string]interface{}); ok {
				if email, ok := m["email"].(string); ok && email != "" {
					return cellPayload{ColumnID: colID, ObjectValue: contactObject{ObjectType: "CONTACT", Email: email}}
				}
			}
			if s, ok := item.(string); ok && s != "" {
				return cellPayload{ColumnID: colID, ObjectValue: contactObject{ObjectType: "CONTACT", Email: s}}
			}
		}
	case map[string]interface{}:
		if email, ok := val["email"].(string); ok && email != "" {
			return cellPayload{ColumnID: colID, ObjectValue: contactObject{ObjectType: "CONTACT", Email: email}}
		}
	}
	return cellPayload{ColumnID: colID} // empty cell
}

// normalizeCellValue converts Go types that Smartsheet cannot parse into
// types it accepts. Smartsheet cell values must be string, number, or bool.
// Slices are joined as comma-separated strings; contact maps are resolved to
// their email field; anything else is fmt.Sprintf'd.
func normalizeCellValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string, bool, int, int64, float64:
		return v
	case []string:
		return strings.Join(val, ", ")
	case map[string]interface{}:
		// Contact objects from Notion/Jira/Airtable look like {"email": "..."}.
		// Extract the email if present; otherwise fall through to fmt.Sprintf.
		if email, ok := val["email"].(string); ok && email != "" {
			return email
		}
		return fmt.Sprintf("%v", val)
	case []interface{}:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			// Each item may itself be a contact map — resolve it too.
			if m, ok := item.(map[string]interface{}); ok {
				if email, ok := m["email"].(string); ok && email != "" {
					parts = append(parts, email)
					continue
				}
			}
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", val)
	}
}

// CreateWorkspace creates a new Smartsheet workspace and returns its ID.
// If a workspace with the same name already exists it returns the existing ID.
func (l *Loader) CreateWorkspace(ctx context.Context, name string) (int64, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return 0, err
	}
	l.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/workspaces", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("smartsheet create workspace: %s", readAPIError(resp))
	}

	var result struct {
		ResultCode int `json:"resultCode"`
		Result     struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Result.ID, nil
}

func readAPIError(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(body) == 0 {
		return resp.Status
	}
	return fmt.Sprintf("%s: %s", resp.Status, body)
}

const defaultBaseURL = "https://api.smartsheet.com/2.0"

type Loader struct {
	token   string
	baseURL string
	client  *http.Client
	rl      *ratelimit.Limiter
}

type Option func(*Loader)

func WithBaseURL(u string) Option {
	return func(l *Loader) { l.baseURL = u }
}

func New(token string, opts ...Option) *Loader {
	l := &Loader{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
		rl:      ratelimit.ForPlatform("smartsheet"),
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

type columnPayload struct {
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Primary bool     `json:"primary,omitempty"`
	Options []string `json:"options,omitempty"`
}

type createSheetResponse struct {
	ResultCode int `json:"resultCode"`
	Result     struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Permalink string `json:"permalink"`
		Columns   []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"columns"`
	} `json:"result"`
}

// CreateSheet creates a new sheet and returns:
//   - sheet ID
//   - map of column title → column ID
//   - set of column IDs that are CONTACT_LIST (cells must use objectValue)
func (l *Loader) CreateSheet(ctx context.Context, proj *model.Project, workspaceID int64) (int64, map[string]int64, map[int64]bool, error) {
	cols := make([]columnPayload, 0, len(proj.Columns))
	for i, c := range proj.Columns {
		ssType := transformer.ToSmartsheetColumnType(c.Type)
		// Smartsheet rejects PICKLIST/MULTI_PICKLIST with no options — fall back to TEXT_NUMBER
		if (ssType == "PICKLIST" || ssType == "MULTI_PICKLIST") && len(c.Options) == 0 {
			ssType = "TEXT_NUMBER"
		}
		// Smartsheet does not support DATETIME as a column type — use DATE instead.
		if ssType == "DATETIME" {
			ssType = "DATE"
		}
		// MULTI_CONTACT_LIST is not supported; use CONTACT_LIST.
		if ssType == "MULTI_CONTACT_LIST" {
			ssType = "CONTACT_LIST"
		}
		cp := columnPayload{
			Title:   c.Name,
			Type:    ssType,
			Options: c.Options,
		}
		if i == 0 {
			cp.Primary = true
		}
		cols = append(cols, cp)
	}

	body, err := json.Marshal(map[string]interface{}{"name": proj.Name, "columns": cols})
	if err != nil {
		return 0, nil, nil, err
	}

	url := l.baseURL + "/sheets"
	if workspaceID != 0 {
		url = fmt.Sprintf("%s/workspaces/%d/sheets", l.baseURL, workspaceID)
	}

	l.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return 0, nil, nil, fmt.Errorf("smartsheet create sheet: %s", readAPIError(resp))
	}

	var result createSheetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, nil, nil, err
	}
	if result.ResultCode != 0 {
		return 0, nil, nil, fmt.Errorf("smartsheet create sheet failed, resultCode=%d", result.ResultCode)
	}

	colMap := make(map[string]int64, len(result.Result.Columns))
	contactColIDs := map[int64]bool{}
	for _, c := range result.Result.Columns {
		colMap[c.Title] = c.ID
		if c.Type == "CONTACT_LIST" {
			contactColIDs[c.ID] = true
		}
	}
	return result.Result.ID, colMap, contactColIDs, nil
}

type cellPayload struct {
	ColumnID    int64       `json:"columnId"`
	Value       interface{} `json:"value,omitempty"`
	ObjectValue interface{} `json:"objectValue,omitempty"`
}

type contactObject struct {
	ObjectType string `json:"objectType"`
	Email      string `json:"email"`
	Name       string `json:"name,omitempty"`
}

type rowPayload struct {
	ToBottom bool          `json:"toBottom"`
	ParentID *int64        `json:"parentId,omitempty"`
	Cells    []cellPayload `json:"cells"`
}

type insertRowsResponse struct {
	Result []struct {
		ID int64 `json:"id"`
	} `json:"result"`
}

// BulkInsertRows inserts rows in batches of 500.
// contactColIDs identifies columns that require objectValue (CONTACT_LIST).
// Returns a map of source row ID → Smartsheet row ID.
func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, contactColIDs map[int64]bool) (map[string]int64, error) {
	rowIDMap := make(map[string]int64)

	var topLevel, children []model.Row
	for _, r := range rows {
		if r.ParentID == "" {
			topLevel = append(topLevel, r)
		} else {
			children = append(children, r)
		}
	}

	if err := l.insertInBatches(ctx, sheetID, topLevel, colIndexByName, contactColIDs, rowIDMap, nil); err != nil {
		return rowIDMap, err
	}
	if err := l.insertInBatches(ctx, sheetID, children, colIndexByName, contactColIDs, rowIDMap, rowIDMap); err != nil {
		return rowIDMap, err
	}

	return rowIDMap, nil
}

func (l *Loader) insertInBatches(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, contactColIDs map[int64]bool, rowIDMap map[string]int64, parentIDMap map[string]int64) error {
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batchMap, err := l.insertRowBatch(ctx, sheetID, rows[i:end], colIndexByName, contactColIDs, parentIDMap)
		if err != nil {
			return fmt.Errorf("batch %d: %w", i/batchSize, err)
		}
		for k, v := range batchMap {
			rowIDMap[k] = v
		}
	}
	return nil
}

// UploadAttachment uploads a file to a specific row in a Smartsheet sheet.
// The attachment is posted as a multipart/form-data request.
func (l *Loader) UploadAttachment(ctx context.Context, sheetID, rowID int64, filename, contentType string, body io.Reader) error {
	url := fmt.Sprintf("%s/sheets/%d/rows/%d/attachments", l.baseURL, sheetID, rowID)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		defer func() { _ = pw.Close() }()
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, body); err != nil {
			pw.CloseWithError(err)
			return
		}
		_ = mw.Close()
	}()

	l.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("smartsheet attachment upload: %s", readAPIError(resp))
	}
	return nil
}

// AddComment posts a comment to a row's discussion thread.
// Smartsheet API: POST /sheets/{sheetId}/rows/{rowId}/discussions
func (l *Loader) AddComment(ctx context.Context, sheetID, rowID int64, text string) error {
	url := fmt.Sprintf("%s/sheets/%d/rows/%d/discussions", l.baseURL, sheetID, rowID)

	payload := map[string]interface{}{
		"comment": map[string]string{
			"text": text,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	l.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("smartsheet add comment: %s", readAPIError(resp))
	}
	return nil
}

func (l *Loader) insertRowBatch(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, contactColIDs map[int64]bool, parentIDMap map[string]int64) (map[string]int64, error) {
	rowPayloads := make([]rowPayload, 0, len(rows))
	for _, r := range rows {
		cells := make([]cellPayload, 0, len(r.Cells))
		for _, c := range r.Cells {
			colID, ok := colIndexByName[c.ColumnName]
			if !ok {
				continue
			}
			if contactColIDs[colID] {
				// CONTACT_LIST columns require objectValue format.
				cp := contactCell(colID, c.Value)
				if cp.ObjectValue == nil {
					continue // skip empty contact cells — sending no value/objectValue causes 1012
				}
				cells = append(cells, cp)
			} else {
				v := normalizeCellValue(c.Value)
				if v == nil {
					continue // skip nil-value cells
				}
				cells = append(cells, cellPayload{ColumnID: colID, Value: v})
			}
		}
		rp := rowPayload{ToBottom: true, Cells: cells}
		if parentIDMap != nil && r.ParentID != "" {
			if ssParentID, ok := parentIDMap[r.ParentID]; ok {
				rp.ParentID = &ssParentID
				rp.ToBottom = false
			}
		}
		rowPayloads = append(rowPayloads, rp)
	}

	body, err := json.Marshal(rowPayloads)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/sheets/%d/rows", l.baseURL, sheetID)

	// Retry once on 404 — Smartsheet occasionally takes a moment to propagate a
	// newly-created sheet (especially when created inside a workspace).
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			// Brief pause before retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		l.rl.Wait()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+l.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err = l.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusNotFound {
			break
		}
		_ = resp.Body.Close()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("smartsheet row insert: %s", readAPIError(resp))
	}

	var result insertRowsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	rowIDMap := make(map[string]int64, len(rows))
	for i, r := range rows {
		if i < len(result.Result) {
			rowIDMap[r.ID] = result.Result[i].ID
		}
	}
	return rowIDMap, nil
}
