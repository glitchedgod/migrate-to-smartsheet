package model

import "time"

type ColumnType string

const (
	TypeText         ColumnType = "Text"
	TypeNumber       ColumnType = "Number"
	TypeDate         ColumnType = "Date"
	TypeDateTime     ColumnType = "DateTime"
	TypeCheckbox     ColumnType = "Checkbox"
	TypeSingleSelect ColumnType = "SingleSelect"
	TypeMultiSelect  ColumnType = "MultiSelect"
	TypeContact      ColumnType = "Contact"
	TypeMultiContact ColumnType = "MultiContact"
	TypeURL          ColumnType = "URL"
	TypeDuration     ColumnType = "Duration"
)

type ColumnDef struct {
	Name    string
	Type    ColumnType
	Options []string
}

type Cell struct {
	ColumnName string
	Value      interface{}
}

type Attachment struct {
	Name        string
	URL         string
	ContentType string
	SizeBytes   int64
	ExpiresAt   time.Time
}

type Comment struct {
	AuthorEmail string
	AuthorName  string
	Body        string
	CreatedAt   time.Time
}

type Row struct {
	ID          string
	ParentID    string
	Cells       []Cell
	Attachments []Attachment
	Comments    []Comment
}

type Project struct {
	ID      string
	Name    string
	Columns []ColumnDef
	Rows    []Row
}

type Workspace struct {
	ID       string
	Name     string
	Projects []Project
}
