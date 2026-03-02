package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/extractor"
	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
	"github.com/bchauhan/migrate-to-smartsheet/internal/transformer"
	"github.com/bchauhan/migrate-to-smartsheet/pkg/model"
)

const defaultBaseURL = "https://api.monday.com/v2"

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
		rl:      ratelimit.ForPlatform("monday"),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) query(ctx context.Context, q string, out interface{}) error {
	e.rl.Wait()
	body, err := json.Marshal(map[string]string{"query": q})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", e.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Version", "2024-01")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var wrapper struct {
		Data   json.RawMessage `json:"data"`
		Errors []interface{}   `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return err
	}
	if len(wrapper.Errors) > 0 {
		return fmt.Errorf("monday.com error: %v", wrapper.Errors[0])
	}
	return json.Unmarshal(wrapper.Data, out)
}

func (e *Extractor) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var data struct {
		Workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workspaces"`
	}
	if err := e.query(ctx, `{ workspaces { id name } }`, &data); err != nil {
		return nil, err
	}
	ws := make([]model.Workspace, len(data.Workspaces))
	for i, w := range data.Workspaces {
		ws[i] = model.Workspace{ID: w.ID, Name: w.Name}
	}
	return ws, nil
}

func (e *Extractor) ExtractProject(ctx context.Context, workspaceID, boardID string, opts extractor.Options) (*model.Project, error) {
	var data struct {
		Boards []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Columns []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"columns"`
			ItemsPage struct {
				Items []struct {
					ID           string `json:"id"`
					Name         string `json:"name"`
					ColumnValues []struct {
						ID   string `json:"id"`
						Text string `json:"text"`
					} `json:"column_values"`
				} `json:"items"`
			} `json:"items_page"`
		} `json:"boards"`
	}

	q := fmt.Sprintf(`{ boards(ids: [%s]) { id name columns { id title type } items_page(limit: 500) { items { id name column_values { id text } } } } }`, boardID)
	if err := e.query(ctx, q, &data); err != nil {
		return nil, err
	}
	if len(data.Boards) == 0 {
		return nil, fmt.Errorf("board %s not found", boardID)
	}
	board := data.Boards[0]

	columns := make([]model.ColumnDef, 0, len(board.Columns))
	for _, c := range board.Columns {
		colType := model.TypeText
		switch c.Type {
		case "color", "status":
			colType = model.TypeSingleSelect
		case "date":
			colType = model.TypeDate
		case "checkbox":
			colType = model.TypeCheckbox
		case "people", "multiple-person":
			colType = model.TypeMultiContact
		}
		columns = append(columns, model.ColumnDef{Name: c.Title, Type: colType})
	}

	rows := make([]model.Row, 0, len(board.ItemsPage.Items))
	for _, item := range board.ItemsPage.Items {
		cells := []model.Cell{{ColumnName: "Name", Value: item.Name}}
		for _, cv := range item.ColumnValues {
			if text := transformer.StripHTML(cv.Text); text != "" {
				cells = append(cells, model.Cell{ColumnName: cv.ID, Value: text})
			}
		}
		rows = append(rows, model.Row{ID: item.ID, Cells: cells})
	}

	return &model.Project{ID: boardID, Name: board.Name, Columns: columns, Rows: rows}, nil
}
