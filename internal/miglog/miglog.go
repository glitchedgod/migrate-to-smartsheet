// Package miglog writes structured NDJSON migration logs to a file.
// Each line is a self-contained JSON object (one per log entry).
// Example line:
//
//	{"ts":"2026-03-05T14:30:01Z","level":"INFO","platform":"asana","project":"My Project","msg":"sheet migration started","fields":{"columns":8,"rows":243}}
package miglog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Level is the severity of a log entry.
type Level string

const (
	LevelInfo    Level = "INFO"
	LevelWarn    Level = "WARN"
	LevelError   Level = "ERROR"
	LevelSummary Level = "SUMMARY"
)

// Entry is one line in the structured log file.
type Entry struct {
	Timestamp time.Time      `json:"ts"`
	Level     Level          `json:"level"`
	Platform  string         `json:"platform,omitempty"`
	Project   string         `json:"project,omitempty"`
	Message   string         `json:"msg"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Logger writes structured NDJSON log entries to a file.
// All public methods are safe to call from multiple goroutines.
type Logger struct {
	path     string
	platform string
	f        *os.File
	w        *bufio.Writer
	mu       sync.Mutex
}

// New creates (or truncates) the log file at path and returns a Logger.
// The caller must call Close() when done to flush and close the file.
func New(path, platform string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("miglog: create %s: %w", path, err)
	}
	return &Logger{
		path:     path,
		platform: platform,
		f:        f,
		w:        bufio.NewWriterSize(f, 64*1024),
	}, nil
}

// FilePath returns the path of the log file.
func (l *Logger) FilePath() string { return l.path }

// Info logs a message at INFO level.
// fields is an optional flat sequence of key, value pairs:
//
//	l.Info("sheet started", "rows", 42, "cols", 8)
func (l *Logger) Info(msg string, fields ...any) {
	l.write(LevelInfo, "", msg, fields...)
}

// Warn logs at WARN level.
func (l *Logger) Warn(msg string, fields ...any) {
	l.write(LevelWarn, "", msg, fields...)
}

// Error logs at ERROR level.
func (l *Logger) Error(msg string, fields ...any) {
	l.write(LevelError, "", msg, fields...)
}

// SheetStart records the beginning of a sheet migration.
func (l *Logger) SheetStart(project string, cols, rows int) {
	l.write(LevelInfo, project, "sheet migration started",
		"columns", cols, "rows", rows)
}

// SheetComplete records a successfully migrated sheet.
func (l *Logger) SheetComplete(project string, rows, attachments, comments int) {
	l.write(LevelInfo, project, "sheet migration completed",
		"rows_migrated", rows,
		"attachments_migrated", attachments,
		"comments_migrated", comments)
}

// SheetFailed records a sheet-level failure.
func (l *Logger) SheetFailed(project string, err error) {
	l.write(LevelError, project, "sheet migration failed", "error", err.Error())
}

// AttachmentSkipped records a skipped attachment with a reason.
func (l *Logger) AttachmentSkipped(project, name, reason string) {
	l.write(LevelWarn, project, "attachment skipped",
		"attachment", name, "reason", reason)
}

// AttachmentFailed records an attachment upload failure.
func (l *Logger) AttachmentFailed(project, name string, err error) {
	l.write(LevelError, project, "attachment upload failed",
		"attachment", name, "error", err.Error())
}

// CommentFailed records a comment post failure.
func (l *Logger) CommentFailed(project string, err error) {
	l.write(LevelError, project, "comment post failed", "error", err.Error())
}

// Summary writes the final migration summary as the last log entry.
func (l *Logger) Summary(sheets, rows, warnings, errors int, status string) {
	l.write(LevelSummary, "", "migration complete",
		"sheets", sheets,
		"rows", rows,
		"warnings", warnings,
		"errors", errors,
		"status", status)
}

// ErrorEntry represents a single ERROR-level entry read back from the log file.
type ErrorEntry struct {
	Sheet   string
	Message string
	ErrText string
}

// ListErrors reads back the NDJSON log file and returns all entries at ERROR level.
// It opens the file independently of the write-side bufio.Writer so it is safe to
// call after Close() — or mid-run if the caller has flushed.
func (l *Logger) ListErrors() []ErrorEntry {
	f, err := os.Open(l.path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var out []ErrorEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Use a minimal struct to avoid pulling in all fields.
		var raw struct {
			Level  string `json:"level"`
			Sheet  string `json:"sheet"`
			Msg    string `json:"msg"`
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw.Level != string(LevelError) {
			continue
		}
		// "sheet" may appear as a top-level field (from project) or inside fields.
		sheet := raw.Sheet
		if sheet == "" {
			// Fall back to "project" top-level field used by write().
			var withProject struct {
				Project string `json:"project"`
			}
			_ = json.Unmarshal(line, &withProject)
			sheet = withProject.Project
		}
		var errText string
		if raw.Fields != nil {
			if v, ok := raw.Fields["error"]; ok {
				_ = json.Unmarshal(v, &errText)
			}
		}
		out = append(out, ErrorEntry{
			Sheet:   sheet,
			Message: raw.Msg,
			ErrText: errText,
		})
	}
	return out
}

// Close flushes the buffer and closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.w.Flush(); err != nil {
		_ = l.f.Close()
		return fmt.Errorf("miglog: flush: %w", err)
	}
	return l.f.Close()
}

// write is the internal append method.
func (l *Logger) write(level Level, project, msg string, kvpairs ...any) {
	entry := Entry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Platform:  l.platform,
		Project:   project,
		Message:   msg,
	}
	if len(kvpairs) >= 2 {
		entry.Fields = make(map[string]any, len(kvpairs)/2)
		for i := 0; i+1 < len(kvpairs); i += 2 {
			key, ok := kvpairs[i].(string)
			if !ok {
				key = fmt.Sprintf("%v", kvpairs[i])
			}
			entry.Fields[key] = kvpairs[i+1]
		}
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(data)
	_ = l.w.WriteByte('\n')
	// Flush immediately after ERROR and SUMMARY so crashes don't lose critical entries.
	if level == LevelError || level == LevelSummary {
		_ = l.w.Flush()
	}
}
