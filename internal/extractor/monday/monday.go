package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/extractor"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/glitchedgod/migrate-to-smartsheet/internal/transformer"
	"github.com/glitchedgod/migrate-to-smartsheet/pkg/model"
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
	defer func() { _ = resp.Body.Close() }()

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
	// Fetch columns (with settings_str for dropdown options) + first page of items.
	// We request both `text` (display string) and `value` (raw JSON) so that
	// people/owner columns — which return empty `text` — can be decoded from `value`.
	var data struct {
		Boards []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Columns []struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Type        string `json:"type"`
				SettingsStr string `json:"settings_str"`
			} `json:"columns"`
			ItemsPage struct {
				Cursor *string `json:"cursor"`
				Items  []struct {
					ID           string `json:"id"`
					Name         string `json:"name"`
					ColumnValues []struct {
						ID    string `json:"id"`
						Text  string `json:"text"`
						Value string `json:"value"`
					} `json:"column_values"`
				} `json:"items"`
			} `json:"items_page"`
		} `json:"boards"`
	}

	q := fmt.Sprintf(`{ boards(ids: [%s]) { id name columns { id title type settings_str } items_page(limit: 500) { cursor items { id name column_values { id text value } } } } }`, boardID)
	if err := e.query(ctx, q, &data); err != nil {
		return nil, err
	}
	if len(data.Boards) == 0 {
		return nil, fmt.Errorf("board %s not found", boardID)
	}
	board := data.Boards[0]

	// Collect all items across pages.
	type rawCV struct {
		ID    string
		Text  string
		Value string
	}
	type item struct {
		ID           string
		Name         string
		ColumnValues []rawCV
	}
	allItems := make([]item, 0, len(board.ItemsPage.Items))
	for _, it := range board.ItemsPage.Items {
		cvs := make([]rawCV, len(it.ColumnValues))
		for i, cv := range it.ColumnValues {
			cvs[i] = rawCV{cv.ID, cv.Text, cv.Value}
		}
		allItems = append(allItems, item{ID: it.ID, Name: it.Name, ColumnValues: cvs})
	}

	cursor := board.ItemsPage.Cursor
	for cursor != nil {
		var pageData struct {
			NextItemsPage struct {
				Cursor *string `json:"cursor"`
				Items  []struct {
					ID           string `json:"id"`
					Name         string `json:"name"`
					ColumnValues []struct {
						ID    string `json:"id"`
						Text  string `json:"text"`
						Value string `json:"value"`
					} `json:"column_values"`
				} `json:"items"`
			} `json:"next_items_page"`
		}
		pageQ := fmt.Sprintf(`{ next_items_page(limit: 500, cursor: "%s") { cursor items { id name column_values { id text value } } } }`, *cursor)
		if err := e.query(ctx, pageQ, &pageData); err != nil {
			return nil, err
		}
		for _, it := range pageData.NextItemsPage.Items {
			cvs := make([]rawCV, len(it.ColumnValues))
			for i, cv := range it.ColumnValues {
				cvs[i] = rawCV{cv.ID, cv.Text, cv.Value}
			}
			allItems = append(allItems, item{ID: it.ID, Name: it.Name, ColumnValues: cvs})
		}
		cursor = pageData.NextItemsPage.Cursor
	}

	// Build column metadata maps.
	colTypeByID := make(map[string]string, len(board.Columns))
	colTitleByID := make(map[string]string, len(board.Columns))
	for _, c := range board.Columns {
		colTypeByID[c.ID] = c.Type
		colTitleByID[c.ID] = c.Title
	}

	// Collect all unique Monday user IDs referenced in people/owner columns so we
	// can resolve them to emails in a single batch query.
	userIDSet := map[string]bool{}
	for _, it := range allItems {
		for _, cv := range it.ColumnValues {
			if t := colTypeByID[cv.ID]; t == "people" || t == "multiple-person" {
				for _, uid := range parsePeopleIDs(cv.Value) {
					userIDSet[uid] = true
				}
			}
		}
	}
	emailByUserID := map[string]string{}
	if len(userIDSet) > 0 {
		emailByUserID = e.resolveUserEmails(ctx, userIDSet)
	}

	// Monday.com item name is a top-level field, not in board.Columns — add it first.
	columns := []model.ColumnDef{{Name: "Name", Type: model.TypeText}}
	for _, c := range board.Columns {
		// Skip the implicit "name" column if the board happens to include it.
		if c.Type == "name" {
			continue
		}
		colType := model.TypeText
		var options []string
		switch c.Type {
		case "color", "status":
			colType = model.TypeSingleSelect
			// Parse the complete label list from settings_str so the Smartsheet
			// PICKLIST is created with all configured options, not just those
			// that happen to appear in the current rows.
			options = parseStatusLabels(c.SettingsStr)
		case "date":
			colType = model.TypeDate
		case "timeline":
			// Split into two DATE columns: "<Title> Start" and "<Title> End".
			columns = append(columns, model.ColumnDef{Name: c.Title + " Start", Type: model.TypeDate})
			columns = append(columns, model.ColumnDef{Name: c.Title + " End", Type: model.TypeDate})
			continue
		case "checkbox":
			colType = model.TypeCheckbox
		case "people", "multiple-person":
			colType = model.TypeMultiContact
		case "file":
			// File columns are surfaced as row-level attachments, not sheet columns.
			continue
		}
		columns = append(columns, model.ColumnDef{Name: c.Title, Type: colType, Options: options})
	}

	// Collect all asset IDs from file columns across all items so we can resolve
	// their download URLs in a single batch query.
	assetIDSet := map[string]bool{}
	if opts.IncludeAttachments {
		for _, it := range allItems {
			for _, cv := range it.ColumnValues {
				if colTypeByID[cv.ID] == "file" {
					for _, aid := range parseFileAssetIDs(cv.Value) {
						assetIDSet[aid] = true
					}
				}
			}
		}
	}
	assetURLByID := map[string]model.Attachment{}
	if len(assetIDSet) > 0 {
		assetURLByID = e.resolveAssets(ctx, assetIDSet)
	}

	rows := make([]model.Row, 0, len(allItems))
	for _, it := range allItems {
		cells := []model.Cell{{ColumnName: "Name", Value: it.Name}}
		var attachments []model.Attachment
		for _, cv := range it.ColumnValues {
			title := colTitleByID[cv.ID]
			if title == "" {
				title = cv.ID
			}
			colType := colTypeByID[cv.ID]

			// People/owner columns: decode user IDs from value JSON and map to emails.
			if colType == "people" || colType == "multiple-person" {
				emails := resolvePeopleEmails(cv.Value, emailByUserID)
				if len(emails) > 0 {
					cells = append(cells, model.Cell{ColumnName: title, Value: emails})
				}
				continue
			}

			// Timeline columns: split into two DATE cells ("<Title> Start" / "<Title> End").
			if colType == "timeline" {
				start, end := parseTimelineValue(cv.Value)
				if start != "" {
					cells = append(cells, model.Cell{ColumnName: title + " Start", Value: start})
				}
				if end != "" {
					cells = append(cells, model.Cell{ColumnName: title + " End", Value: end})
				}
				continue
			}

			// File columns: resolve each asset to a downloadable attachment.
			if colType == "file" {
				if opts.IncludeAttachments {
					for _, aid := range parseFileAssetIDs(cv.Value) {
						if att, ok := assetURLByID[aid]; ok {
							attachments = append(attachments, att)
						}
					}
				}
				continue
			}

			// All other columns: use the plain-text representation.
			if text := transformer.StripHTML(cv.Text); text != "" {
				cells = append(cells, model.Cell{ColumnName: title, Value: text})
			}
		}
		rows = append(rows, model.Row{ID: it.ID, Cells: cells, Attachments: attachments})
	}

	proj := &model.Project{ID: boardID, Name: board.Name, Columns: columns, Rows: rows}
	// PopulateSelectOptions fills in any options still missing (e.g. status columns
	// where settings_str was empty) by scanning actual row values.
	extractor.PopulateSelectOptions(proj)
	return proj, nil
}

// parseTimelineValue parses a Monday.com timeline column's raw JSON value and
// returns the start and end dates as "YYYY-MM-DD" strings.
// The value format is: {"from":"2024-01-15","to":"2024-01-31"}.
// Returns empty strings if the value is absent or malformed.
func parseTimelineValue(raw string) (start, end string) {
	if raw == "" || raw == "null" {
		return "", ""
	}
	var v struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", ""
	}
	return v.From, v.To
}

// parseStatusLabels extracts the ordered label strings from a Monday.com
// status/color column's settings_str JSON.
// The format is: {"labels":{"0":"Done","1":"Working on it","5":"Stuck",...}}
// Labels are returned in numeric key order; blank labels are skipped.
func parseStatusLabels(settingsStr string) []string {
	if settingsStr == "" || settingsStr == "null" {
		return nil
	}
	var settings struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.Unmarshal([]byte(settingsStr), &settings); err != nil {
		return nil
	}
	// Collect non-empty labels. Monday stores index keys as numeric strings;
	// we sort by key to get a stable, predictable ordering.
	type kv struct {
		key   int
		label string
	}
	kvs := make([]kv, 0, len(settings.Labels))
	for k, v := range settings.Labels {
		if v == "" {
			continue
		}
		var n int
		fmt.Sscanf(k, "%d", &n)
		kvs = append(kvs, kv{n, v})
	}
	// Simple insertion sort — label count is tiny (typically <20).
	for i := 1; i < len(kvs); i++ {
		for j := i; j > 0 && kvs[j].key < kvs[j-1].key; j-- {
			kvs[j], kvs[j-1] = kvs[j-1], kvs[j]
		}
	}
	labels := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		labels = append(labels, kv.label)
	}
	return labels
}

// parsePeopleIDs extracts Monday user IDs from a people column's raw JSON value.
// The value looks like: {"personsAndTeams":[{"id":12345,"kind":"person"},...]}.
// Entries with kind=="team" are skipped — they don't map to individual emails.
func parsePeopleIDs(raw string) []string {
	if raw == "" || raw == "null" {
		return nil
	}
	var v struct {
		PersonsAndTeams []struct {
			ID   json.Number `json:"id"`
			Kind string      `json:"kind"`
		} `json:"personsAndTeams"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	ids := make([]string, 0, len(v.PersonsAndTeams))
	for _, pt := range v.PersonsAndTeams {
		if pt.Kind == "person" {
			ids = append(ids, pt.ID.String())
		}
	}
	return ids
}

// resolveUserEmails queries Monday.com for the email addresses of the given user IDs.
// Returns a map of userID → email. On error it returns an empty map (graceful degradation).
func (e *Extractor) resolveUserEmails(ctx context.Context, idSet map[string]bool) map[string]string {
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	var data struct {
		Users []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"users"`
	}
	q := fmt.Sprintf(`{ users(ids: [%s]) { id email } }`, strings.Join(ids, ", "))
	if err := e.query(ctx, q, &data); err != nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(data.Users))
	for _, u := range data.Users {
		if u.Email != "" {
			result[u.ID] = u.Email
		}
	}
	return result
}

// parseFileAssetIDs extracts Monday asset IDs from a file column's raw JSON value.
// The format is: {"files":[{"assetId":12345678,...},...]}
func parseFileAssetIDs(raw string) []string {
	if raw == "" || raw == "null" {
		return nil
	}
	var v struct {
		Files []struct {
			AssetID json.Number `json:"assetId"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	ids := make([]string, 0, len(v.Files))
	for _, f := range v.Files {
		if s := f.AssetID.String(); s != "" && s != "0" {
			ids = append(ids, s)
		}
	}
	return ids
}

// resolveAssets queries Monday.com for the public download URLs of the given asset IDs.
// Returns a map of assetID → model.Attachment. On error returns an empty map.
func (e *Extractor) resolveAssets(ctx context.Context, idSet map[string]bool) map[string]model.Attachment {
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	var data struct {
		Assets []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			URL         string `json:"public_url"`
			FileSize    int64  `json:"file_size"`
		} `json:"assets"`
	}
	q := fmt.Sprintf(`{ assets(ids: [%s]) { id name public_url file_size } }`, strings.Join(ids, ", "))
	if err := e.query(ctx, q, &data); err != nil {
		return map[string]model.Attachment{}
	}
	result := make(map[string]model.Attachment, len(data.Assets))
	for _, a := range data.Assets {
		if a.URL != "" {
			result[a.ID] = model.Attachment{
				Name:      a.Name,
				URL:       a.URL,
				SizeBytes: a.FileSize,
			}
		}
	}
	return result
}

// resolvePeopleEmails parses a people column's raw JSON value and returns a slice
// of email strings for each resolved person. IDs with no known email are skipped.
func resolvePeopleEmails(raw string, emailByUserID map[string]string) []string {
	ids := parsePeopleIDs(raw)
	if len(ids) == 0 {
		return nil
	}
	emails := make([]string, 0, len(ids))
	for _, id := range ids {
		if email, ok := emailByUserID[id]; ok && email != "" {
			emails = append(emails, email)
		}
	}
	return emails
}

// ListProjects lists all boards in the given workspace.
func (e *Extractor) ListProjects(ctx context.Context, workspaceID string) ([]extractor.ProjectRef, error) {
	var data struct {
		Boards []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"boards"`
	}
	q := `{ boards(limit: 100) { id name } }`
	if workspaceID != "" {
		q = fmt.Sprintf(`{ boards(workspace_ids: [%s], limit: 100) { id name } }`, workspaceID)
	}
	if err := e.query(ctx, q, &data); err != nil {
		return nil, err
	}
	refs := make([]extractor.ProjectRef, len(data.Boards))
	for i, b := range data.Boards {
		refs[i] = extractor.ProjectRef{ID: b.ID, Name: b.Name}
	}
	return refs, nil
}
