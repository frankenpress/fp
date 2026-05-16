// Package list wires the `fp list` subcommand. Pure host-side: walks
// the snapshot output dir via summary.Walk, applies --limit, and
// renders either a human table or a JSON array.
package list

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
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
	// PullDir is the optional second source-of-snapshots (typically
	// ".fp/prod-snapshots/" for content pulled from prod via `fp pull`).
	// Empty → list walks OutputDir only. Pulled snapshots get a
	// "pulled" source tag in the output; OutputDir entries get
	// "committed".
	PullDir string

	// Limit caps the result count. 0 = no cap.
	Limit int

	// Format selects the renderer. "" / "table" → human table;
	// "json" → JSON array (one object per snapshot).
	Format string

	Stdout io.Writer
}

// taggedEntry is a summary.Entry paired with the dir-source label
// ("committed" vs "pulled") so renderers can show provenance.
type taggedEntry struct {
	summary.Entry
	Source string
}

// Run executes the list pipeline.
func Run(opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}

	committed, err := summary.Walk(opts.RepoRoot, opts.OutputDir)
	if err != nil {
		return err
	}

	tagged := make([]taggedEntry, 0, len(committed))
	for _, e := range committed {
		tagged = append(tagged, taggedEntry{Entry: e, Source: "committed"})
	}

	if opts.PullDir != "" {
		pulled, perr := summary.Walk(opts.RepoRoot, opts.PullDir)
		if perr != nil {
			// Missing pull dir is fine — typical pre-first-pull state.
			if !errors.Is(perr, fs.ErrNotExist) {
				return perr
			}
		}
		for _, e := range pulled {
			tagged = append(tagged, taggedEntry{Entry: e, Source: "pulled"})
		}
	}

	// Re-sort across both dirs by Created desc (empty Created last).
	sort.SliceStable(tagged, func(i, j int) bool {
		ci, cj := tagged[i].Manifest.Created, tagged[j].Manifest.Created
		if ci == "" && cj != "" {
			return false
		}
		if cj == "" && ci != "" {
			return true
		}
		if ci == cj {
			return tagged[i].Slug < tagged[j].Slug
		}
		return ci > cj
	})

	if opts.Limit > 0 && len(tagged) > opts.Limit {
		tagged = tagged[:opts.Limit]
	}

	switch opts.Format {
	case "", "table":
		return renderTable(opts.Stdout, tagged, opts.OutputDir, opts.PullDir)
	case "json":
		return renderJSON(opts.Stdout, tagged)
	default:
		return fmt.Errorf("unknown --format %q (valid: table, json)", opts.Format)
	}
}

// renderTable prints a tabwriter-aligned table. Columns: slug, source
// (committed/pulled), created (UTC, friendly), templates / options /
// attachments counts, first line of the designer note (truncated).
func renderTable(w io.Writer, entries []taggedEntry, outputDir, pullDir string) error {
	if len(entries) == 0 {
		hint := outputDir
		if pullDir != "" {
			hint = outputDir + " or " + pullDir
		}
		_, err := fmt.Fprintf(w, "no snapshots under %s. capture one with `fp snapshot` or pull one with `fp pull` first.\n", hint)
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tSOURCE\tCREATED\tT\tO\tA\tNOTE")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			e.Slug,
			e.Source,
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
func renderJSON(w io.Writer, entries []taggedEntry) error {
	out := make([]jsonEntry, 0, len(entries))
	for _, e := range entries {
		m := e.Manifest
		out = append(out, jsonEntry{
			Slug:        e.Slug,
			Source:      e.Source,
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
	Source      string     `json:"source"`
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
