package smartsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
)

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
		} `json:"columns"`
	} `json:"result"`
}

func (l *Loader) CreateSheet(ctx context.Context, proj *model.Project, workspaceID int64) (int64, map[string]int64, error) {
	cols := make([]columnPayload, 0, len(proj.Columns))
	for i, c := range proj.Columns {
		cp := columnPayload{
			Title:   c.Name,
			Type:    transformer.ToSmartsheetColumnType(c.Type),
			Options: c.Options,
		}
		if i == 0 {
			cp.Primary = true
		}
		cols = append(cols, cp)
	}

	body, err := json.Marshal(map[string]interface{}{"name": proj.Name, "columns": cols})
	if err != nil {
		return 0, nil, err
	}

	url := l.baseURL + "/sheets"
	if workspaceID != 0 {
		url = fmt.Sprintf("%s/workspaces/%d/sheets", l.baseURL, workspaceID)
	}

	l.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return 0, nil, fmt.Errorf("smartsheet API error: %s", resp.Status)
	}

	var result createSheetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, nil, err
	}
	if result.ResultCode != 0 {
		return 0, nil, fmt.Errorf("smartsheet create sheet failed, resultCode=%d", result.ResultCode)
	}

	colMap := make(map[string]int64, len(result.Result.Columns))
	for _, c := range result.Result.Columns {
		colMap[c.Title] = c.ID
	}
	return result.Result.ID, colMap, nil
}

type cellPayload struct {
	ColumnID int64       `json:"columnId"`
	Value    interface{} `json:"value"`
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
// Rows with ParentID are inserted in a second pass after top-level rows,
// with parentId set to the Smartsheet ID returned from the first pass.
// Returns a map of source row ID → Smartsheet row ID.
func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) (map[string]int64, error) {
	rowIDMap := make(map[string]int64)

	// Separate top-level and child rows
	var topLevel, children []model.Row
	for _, r := range rows {
		if r.ParentID == "" {
			topLevel = append(topLevel, r)
		} else {
			children = append(children, r)
		}
	}

	// Pass 1: insert top-level rows
	if err := l.insertInBatches(ctx, sheetID, topLevel, colIndexByName, rowIDMap, nil); err != nil {
		return rowIDMap, err
	}

	// Pass 2: insert child rows with resolved parentId
	if err := l.insertInBatches(ctx, sheetID, children, colIndexByName, rowIDMap, rowIDMap); err != nil {
		return rowIDMap, err
	}

	return rowIDMap, nil
}

func (l *Loader) insertInBatches(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, rowIDMap map[string]int64, parentIDMap map[string]int64) error {
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batchMap, err := l.insertRowBatch(ctx, sheetID, rows[i:end], colIndexByName, parentIDMap)
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
		return fmt.Errorf("smartsheet attachment upload error: %s", resp.Status)
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
		return fmt.Errorf("smartsheet add comment error: %s", resp.Status)
	}
	return nil
}

func (l *Loader) insertRowBatch(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64, parentIDMap map[string]int64) (map[string]int64, error) {
	rowPayloads := make([]rowPayload, 0, len(rows))
	for _, r := range rows {
		cells := make([]cellPayload, 0, len(r.Cells))
		for _, c := range r.Cells {
			colID, ok := colIndexByName[c.ColumnName]
			if !ok {
				continue
			}
			cells = append(cells, cellPayload{ColumnID: colID, Value: c.Value})
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
	l.rl.Wait()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("smartsheet row insert error: %s", resp.Status)
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
