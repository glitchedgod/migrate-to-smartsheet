package postmigration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// logEntry mirrors the NDJSON shape written by internal/miglog.
// Only the fields we need for display are decoded; the rest are ignored.
type logEntry struct {
	Timestamp time.Time      `json:"ts"`
	Level     string         `json:"level"`
	Platform  string         `json:"platform,omitempty"`
	Project   string         `json:"project,omitempty"`
	Message   string         `json:"msg"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// ANSI colour helpers — no external dependency required.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiBold   = "\033[1m"
)

func green(s string) string  { return ansiGreen + s + ansiReset }
func red(s string) string    { return ansiRed + s + ansiReset }
func yellow(s string) string { return ansiYellow + s + ansiReset }
func bold(s string) string   { return ansiBold + s + ansiReset }

// sheetState accumulates everything we know about one sheet from the log.
type sheetState struct {
	name        string
	rows        int
	attachments int
	comments    int
	errors      []string
	warnings    []string
	completed   bool
}

// PrintLogSummary reads the NDJSON log at logPath, groups entries by project /
// sheet, and prints a human-readable summary with ANSI colours.
func PrintLogSummary(logPath string) {
	if logPath == "" {
		fmt.Println("  No log file path provided.")
		return
	}

	f, err := os.Open(logPath)
	if err != nil {
		fmt.Printf("  Cannot read log file: %v\n", err)
		return
	}
	defer func() { _ = f.Close() }()

	// Ordered list of sheet names (first-seen order).
	var order []string
	sheets := make(map[string]*sheetState)

	// Summary-level fields from the SUMMARY entry.
	var (
		platform    string
		logDate     string
		totalRows   int
		totalSheets int
		totalErrors int
	)

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e logEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}

		// Capture platform and date from the first entry that has them.
		if platform == "" && e.Platform != "" {
			platform = e.Platform
		}
		if logDate == "" && !e.Timestamp.IsZero() {
			logDate = e.Timestamp.Format("2006-01-02 15:04")
		}

		// Project-scoped entries.
		if e.Project != "" {
			ss := ensureSheet(sheets, &order, e.Project)

			switch e.Message {
			case "sheet migration completed":
				ss.completed = true
				ss.rows = intField(e.Fields, "rows_migrated")
				ss.attachments = intField(e.Fields, "attachments_migrated")
				ss.comments = intField(e.Fields, "comments_migrated")

			case "sheet migration failed":
				if errStr, ok := e.Fields["error"].(string); ok && errStr != "" {
					ss.errors = append(ss.errors, errStr)
				}

			default:
				switch e.Level {
				case "ERROR":
					msg := e.Message
					if errStr, ok := e.Fields["error"].(string); ok && errStr != "" {
						msg = fmt.Sprintf("%s: %s", msg, errStr)
					}
					ss.errors = append(ss.errors, msg)

				case "WARN":
					msg := e.Message
					if reason, ok := e.Fields["reason"].(string); ok && reason != "" {
						msg = fmt.Sprintf("%s (%s)", msg, reason)
					}
					ss.warnings = append(ss.warnings, msg)
				}
			}
		}

		// SUMMARY entry — global counts.
		if e.Level == "SUMMARY" {
			totalSheets = intField(e.Fields, "sheets")
			totalRows = intField(e.Fields, "rows")
			totalErrors = intField(e.Fields, "errors")
		}
	}

	if err := sc.Err(); err != nil {
		fmt.Printf("  Error reading log: %v\n", err)
		return
	}

	// ── Header ────────────────────────────────────────────────────────────────
	header := "Migration log"
	if platform != "" {
		header += " — " + platform
	}
	if logDate != "" {
		header += " — " + logDate
	}
	fmt.Printf("\n  %s\n\n", bold(header))

	// ── Per-sheet blocks ──────────────────────────────────────────────────────
	succeeded := 0
	failed := 0
	for _, name := range order {
		ss := sheets[name]

		if len(ss.errors) > 0 {
			failed++
			fmt.Printf("  %s  %s\n", red("✗"), name)
			for _, e := range ss.errors {
				fmt.Printf("     %s %s\n", red("Error:"), e)
			}
		} else {
			succeeded++
			fmt.Printf("  %s  %s\n", green("✓"), name)
			parts := fmt.Sprintf("%d rows", ss.rows)
			if ss.attachments > 0 {
				parts += fmt.Sprintf(" · %d attachments", ss.attachments)
			}
			if ss.comments > 0 {
				parts += fmt.Sprintf(" · %d comments", ss.comments)
			}
			if len(ss.warnings) > 0 {
				parts += fmt.Sprintf(" · %s", yellow(fmt.Sprintf("%d warning(s)", len(ss.warnings))))
			}
			fmt.Printf("     %s\n", parts)
			for _, w := range ss.warnings {
				fmt.Printf("     %s %s\n", yellow("Warning:"), w)
			}
		}
		fmt.Println()
	}

	// ── Summary line ──────────────────────────────────────────────────────────
	// Prefer the SUMMARY entry counts when available; fall back to what we
	// tallied ourselves.
	if totalSheets == 0 {
		totalSheets = len(order)
	}
	if totalRows == 0 {
		for _, ss := range sheets {
			totalRows += ss.rows
		}
	}
	_ = totalErrors // included in the per-sheet display above

	summaryLine := fmt.Sprintf("%s succeeded · %s failed · %d items total",
		green(fmt.Sprintf("%d", succeeded)),
		red(fmt.Sprintf("%d", failed)),
		totalRows,
	)
	fmt.Printf("  %s: %s\n\n", bold("Summary"), summaryLine)
}

// ensureSheet returns the sheetState for name, creating it if necessary and
// appending the name to order on first encounter.
func ensureSheet(sheets map[string]*sheetState, order *[]string, name string) *sheetState {
	if ss, ok := sheets[name]; ok {
		return ss
	}
	ss := &sheetState{name: name}
	sheets[name] = ss
	*order = append(*order, name)
	return ss
}

// intField safely extracts an int from a map[string]any field.
// JSON numbers decode as float64, so we handle that case explicitly.
func intField(fields map[string]any, key string) int {
	if fields == nil {
		return 0
	}
	v, ok := fields[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
