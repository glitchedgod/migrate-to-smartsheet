package smartsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.smartsheet.com/2.0"

type Loader struct {
	token   string
	baseURL string
	client  *http.Client
}

type Option func(*Loader)

func WithBaseURL(u string) Option {
	return func(l *Loader) { l.baseURL = u }
}

func New(token string, opts ...Option) *Loader {
	l := &Loader{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
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
	} `json:"result"`
}

func (l *Loader) CreateSheet(ctx context.Context, proj *model.Project, workspaceID int64) (int64, error) {
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
		return 0, err
	}

	url := l.baseURL + "/sheets"
	if workspaceID != 0 {
		url = fmt.Sprintf("%s/workspaces/%d/sheets", l.baseURL, workspaceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("smartsheet API error: %s", resp.Status)
	}

	var result createSheetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if result.ResultCode != 0 {
		return 0, fmt.Errorf("smartsheet create sheet failed, resultCode=%d", result.ResultCode)
	}
	return result.Result.ID, nil
}

type cellPayload struct {
	ColumnID int64       `json:"columnId"`
	Value    interface{} `json:"value"`
}

type rowPayload struct {
	ToBottom bool          `json:"toBottom"`
	Cells    []cellPayload `json:"cells"`
}

func (l *Loader) BulkInsertRows(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) error {
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := l.insertRowBatch(ctx, sheetID, rows[i:end], colIndexByName); err != nil {
			return fmt.Errorf("batch %d: %w", i/batchSize, err)
		}
	}
	return nil
}

func (l *Loader) insertRowBatch(ctx context.Context, sheetID int64, rows []model.Row, colIndexByName map[string]int64) error {
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
		rowPayloads = append(rowPayloads, rowPayload{ToBottom: true, Cells: cells})
	}

	body, err := json.Marshal(rowPayloads)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/sheets/%d/rows", l.baseURL, sheetID)
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
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("smartsheet row insert error: %s", resp.Status)
	}
	return nil
}
