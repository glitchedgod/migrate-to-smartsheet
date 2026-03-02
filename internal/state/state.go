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
