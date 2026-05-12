// Package summary parses the manifest.yaml produced by mu-plugin's
// wp fp snapshot and prints a friendly post-capture report.
//
// Per the plan's "Schema-version coupling" decision, the parser
// tolerates any fp.snapshot/v* schema and unknown fields; it only
// reads the fields it needs for the printer and silently ignores
// the rest. The strict schema validator lives in the (future) fp
// validate subcommand.
//
// If the schema version is newer than knownMaxSchemaMinor, Print
// prepends a one-line warning so designers know to brew upgrade fp
// — the snapshot itself is fine, but some summary fields might not
// render.
package summary

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// knownMaxSchemaMinor tracks the highest fp.snapshot/v<N> minor this
// fp build was tested against. Bump in lockstep with mu-plugin schema
// bumps that fp wants to surface new fields for.
const knownMaxSchemaMinor = 4

// Manifest is the subset of the fp.snapshot/vN manifest fp reads to
// produce the post-capture summary. Unknown fields in the YAML are
// ignored — gopkg.in/yaml.v3 doesn't fail on extras by default, which
// is exactly the tolerant behaviour the plan calls for.
type Manifest struct {
	Schema   string `yaml:"schema"`
	ID       string `yaml:"id"`
	Created  string `yaml:"created"`
	Source   Source `yaml:"source"`
	Author   Author `yaml:"author"`
	Adapter  string `yaml:"adapter"`
	Scope    Scope  `yaml:"scope"`
	Contents Counts `yaml:"contents"`
}

// Source mirrors manifest.source.{site_url,wp_version,source_theme}.
type Source struct {
	SiteURL     string `yaml:"site_url"`
	WPVersion   string `yaml:"wp_version"`
	SourceTheme string `yaml:"source_theme"`
}

// Author mirrors manifest.author.note.
type Author struct {
	Note string `yaml:"note"`
}

// Scope mirrors the manifest.scope block — only the post-type lists
// are summarised; the option lists are not enumerated to keep the
// printout tight.
type Scope struct {
	PostTypesAdditive []string `yaml:"post_types_additive"`
	PostTypesOwned    []string `yaml:"post_types_owned"`
}

// Counts mirrors the manifest.contents.* numeric fields.
type Counts struct {
	WXRPostCount       int `yaml:"wxr_post_count"`
	TemplatesCount     int `yaml:"templates_count"`
	OptionsCount       int `yaml:"options_count"`
	AttachmentsCount   int `yaml:"attachments_count"`
	BinariesFileCount  int `yaml:"binaries_file_count"`
	BinariesTotalBytes int `yaml:"binaries_total_bytes"`
	UploadsFileCount   int `yaml:"uploads_file_count"`
	UploadsTotalBytes  int `yaml:"uploads_total_bytes"`
}

// Read parses the manifest at path.
func Read(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("summary: parse manifest at %s: %w", path, err)
	}
	return &m, nil
}

// Print renders the post-capture summary to w. relPath is the
// designer-facing path to the snapshot directory (e.g.
// "web/imports/sts-launch"); it's used verbatim in the "manifest:"
// line + the "next steps" copy block.
func Print(w io.Writer, m *Manifest, slug, relPath string) {
	if warn := schemaWarning(m.Schema); warn != "" {
		fmt.Fprintln(w, warn)
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "captured snapshot: %s\n", slug)
	fmt.Fprintf(w, "  manifest:        %s/manifest.yaml\n", relPath)
	if m.Schema != "" {
		fmt.Fprintf(w, "  schema:          %s\n", m.Schema)
	}
	if m.Adapter != "" {
		fmt.Fprintf(w, "  adapter:         %s\n", m.Adapter)
	}
	if m.Source.SourceTheme != "" {
		fmt.Fprintf(w, "  source theme:    %s\n", m.Source.SourceTheme)
	}
	if owned := m.Scope.PostTypesOwned; len(owned) > 0 {
		fmt.Fprintf(w, "  templates:       %d (%s)\n", m.Contents.TemplatesCount, strings.Join(owned, " + "))
	} else if m.Contents.TemplatesCount > 0 {
		fmt.Fprintf(w, "  templates:       %d\n", m.Contents.TemplatesCount)
	}
	if m.Contents.OptionsCount > 0 {
		fmt.Fprintf(w, "  options:         %d\n", m.Contents.OptionsCount)
	}
	if m.Contents.AttachmentsCount > 0 {
		fmt.Fprintf(w, "  attachments:     %d -> %s binaries\n",
			m.Contents.AttachmentsCount,
			humanBytes(m.Contents.BinariesTotalBytes),
		)
	}
	if m.Contents.UploadsFileCount > 0 {
		fmt.Fprintf(w, "  uploads audit:   %d files, %s\n",
			m.Contents.UploadsFileCount,
			humanBytes(m.Contents.UploadsTotalBytes),
		)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "next steps:")
	fmt.Fprintf(w, "  git add %s/\n", relPath)
	note := strings.TrimSpace(m.Author.Note)
	if note == "" {
		fmt.Fprintf(w, "  git commit -m \"snapshot: %s — <your note>\"\n", slug)
	} else {
		// Show just the first line — multi-line notes survive in the
		// manifest but the suggested commit subject is single-line.
		firstLine := strings.SplitN(note, "\n", 2)[0]
		fmt.Fprintf(w, "  git commit -m %q\n", fmt.Sprintf("snapshot: %s — %s", slug, firstLine))
	}
	fmt.Fprintln(w, "  git push && gh pr create")
}

// schemaWarning returns the plan's exact warning text when schema is
// "fp.snapshot/v<N>" with N > knownMaxSchemaMinor.
func schemaWarning(schema string) string {
	const prefix = "fp.snapshot/v"
	if !strings.HasPrefix(schema, prefix) {
		return ""
	}
	tail := schema[len(prefix):]
	n, err := strconv.Atoi(tail)
	if err != nil || n <= knownMaxSchemaMinor {
		return ""
	}
	return fmt.Sprintf(
		"warn: snapshot schema %s is newer than this fp build understands; summary fields may be incomplete (the snapshot itself is fine). consider upgrading: brew upgrade fp",
		schema,
	)
}

// humanBytes renders a byte count with KB / MB suffixes (binary base).
func humanBytes(n int) string {
	const (
		_  = iota
		kb = 1 << (10 * iota)
		mb
		gb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
