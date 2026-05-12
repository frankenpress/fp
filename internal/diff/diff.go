// Package diff compares two committed snapshot directories on the
// host filesystem and produces a structural delta of manifest fields,
// templates, options, theme_mods, attachments, and uploads.
//
// Pure host-side — no container, no docker, no git. Reads each
// snapshot's JSON + manifest.yaml sidecars and structurally diffs.
//
// The output is intended for PR review and designer iteration ("what
// changed between sts-launch yesterday and sts-launch today?"),
// not as a regression test signal — the diff is one-way structural,
// not a content-equivalence checker.
package diff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/frankenpress/fp/internal/summary"
)

// Snapshot is the parsed view of one snapshot directory. It carries
// the subset of fields fp diff actually compares; the on-disk JSON
// has more keys we ignore.
type Snapshot struct {
	Path string

	Manifest *summary.Manifest

	// Templates keyed by "<post_type>/<post_name>".
	Templates map[string]Template

	// Options[key] = JSON value.
	Options map[string]any

	// ThemeMods[stylesheet][key] = JSON value.
	ThemeMods map[string]map[string]any

	// Attachments keyed by _wp_attached_file (relative path).
	Attachments map[string]Attachment

	// Uploads keyed by relative path.
	Uploads map[string]Upload
}

// Template is the subset of an owned-post entry we display in the
// diff. post_content is kept so we can detect "content modified" via
// equality without showing the raw markup.
type Template struct {
	PostType    string
	PostName    string
	PostTitle   string
	PostContent string
	PostStatus  string
	PostExcerpt string
}

// Attachment carries the displayable fields from attachments.json.
type Attachment struct {
	RelativeFile string
	PostTitle    string
	MimeType     string
}

// Upload is one line from uploads-manifest.txt.
type Upload struct {
	Sha256 string
	Size   int64
}

// Read parses a snapshot directory at path. Missing optional sidecars
// (templates.json, options.json, attachments.json, uploads-manifest.txt)
// are tolerated — diff treats them as empty.
func Read(path string) (*Snapshot, error) {
	if path == "" {
		return nil, errors.New("diff.Read: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("snapshot dir not found: %s", abs)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("snapshot path is not a directory: %s", abs)
	}

	snap := &Snapshot{Path: abs}

	manifestPath := filepath.Join(abs, "manifest.yaml")
	m, err := summary.Read(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest at %s: %w", manifestPath, err)
	}
	snap.Manifest = m

	if t, err := readTemplates(filepath.Join(abs, "templates.json")); err != nil {
		return nil, err
	} else {
		snap.Templates = t
	}

	if o, tm, err := readOptions(filepath.Join(abs, "options.json")); err != nil {
		return nil, err
	} else {
		snap.Options = o
		snap.ThemeMods = tm
	}

	if a, err := readAttachments(filepath.Join(abs, "attachments.json")); err != nil {
		return nil, err
	} else {
		snap.Attachments = a
	}

	if u, err := readUploadsManifest(filepath.Join(abs, "uploads-manifest.txt")); err != nil {
		return nil, err
	} else {
		snap.Uploads = u
	}

	return snap, nil
}

func readTemplates(path string) (map[string]Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Template{}, nil
		}
		return nil, err
	}
	// Shape: { "<post_type>": { "<post_name>": { fields... } } }
	var raw map[string]map[string]struct {
		PostTitle   string `json:"post_title"`
		PostContent string `json:"post_content"`
		PostStatus  string `json:"post_status"`
		PostExcerpt string `json:"post_excerpt"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse templates.json at %s: %w", path, err)
	}
	out := make(map[string]Template, 16)
	for postType, bySlug := range raw {
		for slug, fields := range bySlug {
			key := postType + "/" + slug
			out[key] = Template{
				PostType:    postType,
				PostName:    slug,
				PostTitle:   fields.PostTitle,
				PostContent: fields.PostContent,
				PostStatus:  fields.PostStatus,
				PostExcerpt: fields.PostExcerpt,
			}
		}
	}
	return out, nil
}

func readOptions(path string) (map[string]any, map[string]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, map[string]map[string]any{}, nil
		}
		return nil, nil, err
	}
	var raw struct {
		Options   map[string]any            `json:"options"`
		ThemeMods map[string]map[string]any `json:"theme_mods"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse options.json at %s: %w", path, err)
	}
	if raw.Options == nil {
		raw.Options = map[string]any{}
	}
	if raw.ThemeMods == nil {
		raw.ThemeMods = map[string]map[string]any{}
	}
	return raw.Options, raw.ThemeMods, nil
}

func readAttachments(path string) (map[string]Attachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Attachment{}, nil
		}
		return nil, err
	}
	var raw struct {
		ByFile map[string]struct {
			PostTitle    string `json:"post_title"`
			PostMimeType string `json:"post_mime_type"`
		} `json:"by_file"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse attachments.json at %s: %w", path, err)
	}
	out := make(map[string]Attachment, len(raw.ByFile))
	for relFile, fields := range raw.ByFile {
		out[relFile] = Attachment{
			RelativeFile: relFile,
			PostTitle:    fields.PostTitle,
			MimeType:     fields.PostMimeType,
		}
	}
	return out, nil
}

func readUploadsManifest(path string) (map[string]Upload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Upload{}, nil
		}
		return nil, err
	}
	out := make(map[string]Upload, 32)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "<sha256>  <size>  <rel-path>" — two-space separator. Use
		// Fields() (any whitespace) for tolerance against trailing
		// whitespace in the path.
		parts := strings.SplitN(line, "  ", 3)
		if len(parts) != 3 {
			continue
		}
		var size int64
		_, _ = fmt.Sscanf(parts[1], "%d", &size)
		rel := strings.TrimSpace(parts[2])
		out[rel] = Upload{Sha256: parts[0], Size: size}
	}
	return out, nil
}

// Result captures the structural delta between two snapshots. Sets
// are sorted lexicographically by key for deterministic output.
type Result struct {
	A, B *Snapshot

	ManifestChanges []FieldChange

	TemplatesAdded    []string // keys ("<post_type>/<post_name>")
	TemplatesRemoved  []string
	TemplatesModified []TemplateChange

	OptionsAdded   []string
	OptionsRemoved []string
	OptionsChanged []ValueChange

	ThemeModsChanged []ThemeModChange

	AttachmentsAdded   []string
	AttachmentsRemoved []string

	UploadsAdded   []string
	UploadsRemoved []string
	UploadsChanged []UploadChange
}

// FieldChange describes a manifest-level scalar that differs.
type FieldChange struct {
	Field string
	A     string
	B     string
}

// TemplateChange describes a template where post_content differs.
// Title-only changes still count as modified.
type TemplateChange struct {
	Key       string
	PostType  string
	PostName  string
	TitleA    string
	TitleB    string
	BothTitle bool // titles equal — content-only change
}

// ValueChange is a key whose value differs between A and B.
type ValueChange struct {
	Key string
	A   any
	B   any
}

// ThemeModChange is a per-stylesheet mod delta.
type ThemeModChange struct {
	Stylesheet string
	Added      []string
	Removed    []string
	Changed    []ValueChange
}

// UploadChange flags a path whose sha256 or size differs.
type UploadChange struct {
	Path  string
	ShaA  string
	ShaB  string
	SizeA int64
	SizeB int64
}

// Empty reports whether the two snapshots are structurally identical.
func (r *Result) Empty() bool {
	return len(r.ManifestChanges) == 0 &&
		len(r.TemplatesAdded) == 0 && len(r.TemplatesRemoved) == 0 && len(r.TemplatesModified) == 0 &&
		len(r.OptionsAdded) == 0 && len(r.OptionsRemoved) == 0 && len(r.OptionsChanged) == 0 &&
		len(r.ThemeModsChanged) == 0 &&
		len(r.AttachmentsAdded) == 0 && len(r.AttachmentsRemoved) == 0 &&
		len(r.UploadsAdded) == 0 && len(r.UploadsRemoved) == 0 && len(r.UploadsChanged) == 0
}

// Compare returns the structural delta from a to b. a + b must be
// non-nil; pass empty snapshots if comparing against absence.
func Compare(a, b *Snapshot) *Result {
	res := &Result{A: a, B: b}

	// Manifest scalar fields.
	if a.Manifest != nil && b.Manifest != nil {
		if a.Manifest.Schema != b.Manifest.Schema {
			res.ManifestChanges = append(res.ManifestChanges,
				FieldChange{"schema", a.Manifest.Schema, b.Manifest.Schema})
		}
		if a.Manifest.Adapter != b.Manifest.Adapter {
			res.ManifestChanges = append(res.ManifestChanges,
				FieldChange{"adapter", a.Manifest.Adapter, b.Manifest.Adapter})
		}
		if a.Manifest.Source.SourceTheme != b.Manifest.Source.SourceTheme {
			res.ManifestChanges = append(res.ManifestChanges,
				FieldChange{"source_theme", a.Manifest.Source.SourceTheme, b.Manifest.Source.SourceTheme})
		}
	}

	// Templates.
	for k := range b.Templates {
		if _, ok := a.Templates[k]; !ok {
			res.TemplatesAdded = append(res.TemplatesAdded, k)
		}
	}
	for k := range a.Templates {
		if _, ok := b.Templates[k]; !ok {
			res.TemplatesRemoved = append(res.TemplatesRemoved, k)
		}
	}
	for k, ta := range a.Templates {
		tb, ok := b.Templates[k]
		if !ok {
			continue
		}
		if ta.PostContent != tb.PostContent || ta.PostTitle != tb.PostTitle ||
			ta.PostStatus != tb.PostStatus || ta.PostExcerpt != tb.PostExcerpt {
			res.TemplatesModified = append(res.TemplatesModified, TemplateChange{
				Key:       k,
				PostType:  ta.PostType,
				PostName:  ta.PostName,
				TitleA:    ta.PostTitle,
				TitleB:    tb.PostTitle,
				BothTitle: ta.PostTitle == tb.PostTitle,
			})
		}
	}
	sort.Strings(res.TemplatesAdded)
	sort.Strings(res.TemplatesRemoved)
	sort.Slice(res.TemplatesModified, func(i, j int) bool {
		return res.TemplatesModified[i].Key < res.TemplatesModified[j].Key
	})

	// Options.
	for k := range b.Options {
		if _, ok := a.Options[k]; !ok {
			res.OptionsAdded = append(res.OptionsAdded, k)
		}
	}
	for k := range a.Options {
		if _, ok := b.Options[k]; !ok {
			res.OptionsRemoved = append(res.OptionsRemoved, k)
		}
	}
	for k, va := range a.Options {
		vb, ok := b.Options[k]
		if !ok {
			continue
		}
		if !valuesEqual(va, vb) {
			res.OptionsChanged = append(res.OptionsChanged, ValueChange{Key: k, A: va, B: vb})
		}
	}
	sort.Strings(res.OptionsAdded)
	sort.Strings(res.OptionsRemoved)
	sort.Slice(res.OptionsChanged, func(i, j int) bool {
		return res.OptionsChanged[i].Key < res.OptionsChanged[j].Key
	})

	// Theme mods (per-stylesheet).
	stylesheets := map[string]struct{}{}
	for s := range a.ThemeMods {
		stylesheets[s] = struct{}{}
	}
	for s := range b.ThemeMods {
		stylesheets[s] = struct{}{}
	}
	for stylesheet := range stylesheets {
		modA := a.ThemeMods[stylesheet]
		modB := b.ThemeMods[stylesheet]
		change := ThemeModChange{Stylesheet: stylesheet}
		for k := range modB {
			if _, ok := modA[k]; !ok {
				change.Added = append(change.Added, k)
			}
		}
		for k := range modA {
			if _, ok := modB[k]; !ok {
				change.Removed = append(change.Removed, k)
			}
		}
		for k, va := range modA {
			vb, ok := modB[k]
			if !ok {
				continue
			}
			if !valuesEqual(va, vb) {
				change.Changed = append(change.Changed, ValueChange{Key: k, A: va, B: vb})
			}
		}
		if len(change.Added)+len(change.Removed)+len(change.Changed) > 0 {
			sort.Strings(change.Added)
			sort.Strings(change.Removed)
			sort.Slice(change.Changed, func(i, j int) bool {
				return change.Changed[i].Key < change.Changed[j].Key
			})
			res.ThemeModsChanged = append(res.ThemeModsChanged, change)
		}
	}
	sort.Slice(res.ThemeModsChanged, func(i, j int) bool {
		return res.ThemeModsChanged[i].Stylesheet < res.ThemeModsChanged[j].Stylesheet
	})

	// Attachments.
	for k := range b.Attachments {
		if _, ok := a.Attachments[k]; !ok {
			res.AttachmentsAdded = append(res.AttachmentsAdded, k)
		}
	}
	for k := range a.Attachments {
		if _, ok := b.Attachments[k]; !ok {
			res.AttachmentsRemoved = append(res.AttachmentsRemoved, k)
		}
	}
	sort.Strings(res.AttachmentsAdded)
	sort.Strings(res.AttachmentsRemoved)

	// Uploads.
	for k := range b.Uploads {
		if _, ok := a.Uploads[k]; !ok {
			res.UploadsAdded = append(res.UploadsAdded, k)
		}
	}
	for k := range a.Uploads {
		if _, ok := b.Uploads[k]; !ok {
			res.UploadsRemoved = append(res.UploadsRemoved, k)
		}
	}
	for k, ua := range a.Uploads {
		ub, ok := b.Uploads[k]
		if !ok {
			continue
		}
		if ua.Sha256 != ub.Sha256 || ua.Size != ub.Size {
			res.UploadsChanged = append(res.UploadsChanged, UploadChange{
				Path: k,
				ShaA: ua.Sha256, ShaB: ub.Sha256,
				SizeA: ua.Size, SizeB: ub.Size,
			})
		}
	}
	sort.Strings(res.UploadsAdded)
	sort.Strings(res.UploadsRemoved)
	sort.Slice(res.UploadsChanged, func(i, j int) bool {
		return res.UploadsChanged[i].Path < res.UploadsChanged[j].Path
	})

	return res
}

// valuesEqual compares two JSON-decoded values structurally. Cheap
// implementation via re-marshal — fine for the small option payloads
// snapshots carry.
func valuesEqual(a, b any) bool {
	ba, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ba) == string(bb)
}
