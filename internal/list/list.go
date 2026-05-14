// Package list wires the `fp list` subcommand. Pure host-side: walks
// the snapshot output dir via summary.Walk, applies --limit, and
// renders either a human table or a JSON array.
package list

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/frankenpress/fp/internal/summary"
)

// Options carries every input fp list needs. Built from flags + config
// in internal/cli/list.go.
type Options struct {
	RepoRoot  string
	OutputDir string

	// Limit caps the result count. 0 = no cap.
	Limit int

	// Format selects the renderer. "" / "table" → human table;
	// "json" → JSON array (one object per snapshot).
	Format string

	Stdout io.Writer
}

// Run executes the list pipeline.
func Run(opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}

	entries, err := summary.Walk(opts.RepoRoot, opts.OutputDir)
	if err != nil {
		return err
	}
	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[:opts.Limit]
	}

	switch opts.Format {
	case "", "table":
		return renderTable(opts.Stdout, entries, opts.OutputDir)
	case "json":
		return renderJSON(opts.Stdout, entries)
	default:
		return fmt.Errorf("unknown --format %q (valid: table, json)", opts.Format)
	}
}

// renderTable prints a tabwriter-aligned table. Columns: slug, created
// (UTC, friendly), templates / options / attachments counts, first
// line of the designer note (truncated to noteMaxLen).
func renderTable(w io.Writer, entries []summary.Entry, outputDir string) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintf(w, "no snapshots under %s. capture one with `fp snapshot` first.\n", outputDir)
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tCREATED\tT\tO\tA\tNOTE")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\n",
			e.Slug,
			friendlyCreated(e.Manifest.Created),
			e.Manifest.Contents.TemplatesCount,
			e.Manifest.Contents.OptionsCount,
			e.Manifest.Contents.AttachmentsCount,
			truncateNote(e.Manifest.Author.Note, noteMaxLen),
		)
	}
	return tw.Flush()
}

// renderJSON prints a JSON array, one object per snapshot. Field
// names are stable for scripting; absent values are omitted.
func renderJSON(w io.Writer, entries []summary.Entry) error {
	out := make([]jsonEntry, 0, len(entries))
	for _, e := range entries {
		m := e.Manifest
		out = append(out, jsonEntry{
			Slug:        e.Slug,
			HostDir:     e.HostDir,
			Created:     m.Created,
			Schema:      m.Schema,
			Adapter:     m.Adapter,
			SourceTheme: m.Source.SourceTheme,
			Note:        m.Author.Note,
			Counts: jsonCounts{
				Templates:    m.Contents.TemplatesCount,
				Options:      m.Contents.OptionsCount,
				Attachments:  m.Contents.AttachmentsCount,
				UploadsFiles: m.Contents.UploadsFileCount,
				UploadsBytes: m.Contents.UploadsTotalBytes,
			},
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

const noteMaxLen = 48

// friendlyCreated reformats an ISO 8601 UTC timestamp ("2026-05-14T09:18:00Z")
// into "2026-05-14 09:18". Returns "—" when the input is empty (broken
// or pre-v4 manifest). Returns the input verbatim when parsing fails
// so the column still carries diagnostic value.
func friendlyCreated(s string) string {
	if s == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// truncateNote returns the first line of the note, truncated to max
// runes with an ellipsis. Empty note → empty string.
func truncateNote(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

type jsonEntry struct {
	Slug        string     `json:"slug"`
	HostDir     string     `json:"host_dir"`
	Created     string     `json:"created"`
	Schema      string     `json:"schema,omitempty"`
	Adapter     string     `json:"adapter,omitempty"`
	SourceTheme string     `json:"source_theme,omitempty"`
	Note        string     `json:"note,omitempty"`
	Counts      jsonCounts `json:"counts"`
}

type jsonCounts struct {
	Templates    int `json:"templates"`
	Options      int `json:"options"`
	Attachments  int `json:"attachments"`
	UploadsFiles int `json:"uploads_files"`
	UploadsBytes int `json:"uploads_bytes"`
}
