package state

import (
	"encoding/json"
	"os"
	"time"
)

type PartialSheet struct {
	SourceID         string `json:"source_id"`
	SmartsheetID     int64  `json:"smartsheet_id"`
	LastCompletedRow int    `json:"last_completed_row"`
}

type MigrationState struct {
	Source          string        `json:"source"`
	StartedAt       time.Time     `json:"started_at"`
	CompletedSheets []string      `json:"completed_sheets"`
	PartialSheet    *PartialSheet `json:"partial_sheet,omitempty"`
}

func (s *MigrationState) IsCompleted(sourceProjectID string) bool {
	for _, id := range s.CompletedSheets {
		if id == sourceProjectID {
			return true
		}
	}
	return false
}

func (s *MigrationState) MarkCompleted(sourceProjectID string) {
	if s.IsCompleted(sourceProjectID) {
		return
	}
	s.CompletedSheets = append(s.CompletedSheets, sourceProjectID)
}

// ClearCompleted removes a single project ID from the completed list, allowing
// it to be re-migrated (used by the re-run failed sheets feature).
func (s *MigrationState) ClearCompleted(sourceProjectID string) {
	filtered := s.CompletedSheets[:0]
	for _, id := range s.CompletedSheets {
		if id != sourceProjectID {
			filtered = append(filtered, id)
		}
	}
	s.CompletedSheets = filtered
}

// UpdatePartialSheet records progress within a currently-migrating sheet.
func (s *MigrationState) UpdatePartialSheet(sourceID string, smartsheetID int64, lastRow int) {
	s.PartialSheet = &PartialSheet{
		SourceID:         sourceID,
		SmartsheetID:     smartsheetID,
		LastCompletedRow: lastRow,
	}
}

// ClearPartialSheet removes the partial sheet record (called when a sheet completes successfully).
func (s *MigrationState) ClearPartialSheet() {
	s.PartialSheet = nil
}

func Save(path string, s *MigrationState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func Load(path string) (*MigrationState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s MigrationState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
