package transformer

import (
	"encoding/csv"
	"io"
)

type UserMap struct {
	m map[string]string
}

func (u *UserMap) Lookup(sourceID string) string {
	if u.m == nil {
		return ""
	}
	return u.m[sourceID]
}

func LoadUserMapFromReader(r io.Reader) (*UserMap, error) {
	records, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for i, row := range records {
		if i == 0 {
			continue
		}
		if len(row) >= 2 {
			m[row[0]] = row[1]
		}
	}
	return &UserMap{m: m}, nil
}
