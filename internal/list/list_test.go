package list

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_TableFormat_OrderedByCreatedDesc(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "2026-05-14T09-18-00Z", manifestBody{
		ID:        "2026-05-14T09-18-00Z",
		Created:   "2026-05-14T09:18:00Z",
		Templates: 14,
		Options:   23,
		Note:      "homepage hero variant",
	})
	writeManifest(t, root, "sts-launch", manifestBody{
		ID:        "sts-launch",
		Created:   "2026-05-12T14:01:00Z",
		Templates: 12,
		Options:   23,
		Note:      "initial launch design",
	})

	var buf bytes.Buffer
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header + 2 entries):\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "SLUG") {
		t.Errorf("header missing or wrong: %q", lines[0])
	}
	if !strings.Contains(lines[1], "2026-05-14T09-18-00Z") {
		t.Errorf("first row should be the newest snapshot, got: %q", lines[1])
	}
	if !strings.Contains(lines[1], "2026-05-14 09:18") {
		t.Errorf("first row missing friendly created timestamp, got: %q", lines[1])
	}
	if !strings.Contains(lines[1], "homepage hero variant") {
		t.Errorf("first row missing note, got: %q", lines[1])
	}
	if !strings.Contains(lines[2], "sts-launch") {
		t.Errorf("second row should be sts-launch, got: %q", lines[2])
	}
}

func TestRun_JSONFormat_StableShape(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "2026-05-14T09-18-00Z", manifestBody{
		ID:           "2026-05-14T09-18-00Z",
		Created:      "2026-05-14T09:18:00Z",
		Schema:       "fp.snapshot/v5",
		Adapter:      "fse",
		SourceTheme:  "twentytwentyfive",
		Templates:    14,
		Options:      23,
		Attachments:  42,
		UploadsFiles: 81,
		UploadsBytes: 1024 * 1024 * 5,
		Note:         "homepage hero variant",
	})

	var buf bytes.Buffer
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Format:    "json",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	e := got[0]
	if e["slug"] != "2026-05-14T09-18-00Z" {
		t.Errorf("slug = %v, want 2026-05-14T09-18-00Z", e["slug"])
	}
	if e["created"] != "2026-05-14T09:18:00Z" {
		t.Errorf("created = %v, want raw ISO 8601 timestamp", e["created"])
	}
	if e["adapter"] != "fse" {
		t.Errorf("adapter = %v, want fse", e["adapter"])
	}
	if e["source_theme"] != "twentytwentyfive" {
		t.Errorf("source_theme = %v, want twentytwentyfive", e["source_theme"])
	}
	counts, ok := e["counts"].(map[string]any)
	if !ok {
		t.Fatalf("counts wrong shape: %T", e["counts"])
	}
	if counts["templates"].(float64) != 14 {
		t.Errorf("counts.templates = %v, want 14", counts["templates"])
	}
	if counts["uploads_bytes"].(float64) != float64(1024*1024*5) {
		t.Errorf("counts.uploads_bytes wrong: %v", counts["uploads_bytes"])
	}
}

func TestRun_Limit_CapsResultCount(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "snap-1", manifestBody{Created: "2026-05-14T09:18:00Z"})
	writeManifest(t, root, "snap-2", manifestBody{Created: "2026-05-13T09:18:00Z"})
	writeManifest(t, root, "snap-3", manifestBody{Created: "2026-05-12T09:18:00Z"})

	var buf bytes.Buffer
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Limit:     2,
		Format:    "json",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (limit applied)", len(got))
	}
	if got[0]["slug"] != "snap-1" || got[1]["slug"] != "snap-2" {
		t.Errorf("got slugs %v, %v want snap-1, snap-2 (newest first)", got[0]["slug"], got[1]["slug"])
	}
}

func TestRun_EmptyDir_TableShowsHint(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), "no snapshots") {
		t.Errorf("expected 'no snapshots' hint, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "fp snapshot") {
		t.Errorf("expected the suggested-next-command hint, got: %q", buf.String())
	}
}

func TestRun_EmptyDir_JSONReturnsEmptyArray(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Format:    "json",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("expected JSON empty array, got: %q", buf.String())
	}
}

func TestRun_UnknownFormat_Errors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Format:    "yaml",
		Stdout:    &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error message should mention the bad value, got: %v", err)
	}
}

func TestRun_BrokenManifestSurfacedWithDashCreated(t *testing.T) {
	// Manifest exists but missing Created — list should still show it
	// so the designer notices something's wrong, with "—" for created.
	root := t.TempDir()
	dir := filepath.Join(root, "web", "imports", "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: broken\nadapter: fse\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := Run(Options{
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "broken") {
		t.Errorf("expected broken slug in output, got: %q", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("expected em-dash placeholder for missing created, got: %q", out)
	}
}

func TestTruncateNote(t *testing.T) {
	cases := []struct {
		in, want string
		max      int
	}{
		{in: "", max: 10, want: ""},
		{in: "short", max: 10, want: "short"},
		{in: "exactly10c", max: 10, want: "exactly10c"},
		{in: "this is more than ten chars", max: 10, want: "this is m…"},
		{in: "first line\nsecond line", max: 20, want: "first line"},
		{in: "   padded   ", max: 20, want: "padded"},
	}
	for _, c := range cases {
		got := truncateNote(c.in, c.max)
		if got != c.want {
			t.Errorf("truncateNote(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestFriendlyCreated(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "—"},
		{"2026-05-14T09:18:00Z", "2026-05-14 09:18"},
		{"not-a-timestamp", "not-a-timestamp"},
	}
	for _, c := range cases {
		got := friendlyCreated(c.in)
		if got != c.want {
			t.Errorf("friendlyCreated(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// manifestBody is the subset of manifest fields the list-package tests
// need to materialise. Mirrors the summary.Manifest shape but kept
// flat for ergonomic test setup.
type manifestBody struct {
	Schema       string
	ID           string
	Created      string
	Adapter      string
	SourceTheme  string
	Note         string
	Templates    int
	Options      int
	Attachments  int
	UploadsFiles int
	UploadsBytes int
}

func writeManifest(t *testing.T, root, slug string, b manifestBody) {
	t.Helper()
	dir := filepath.Join(root, "web", "imports", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	schema := b.Schema
	if schema == "" {
		schema = "fp.snapshot/v5"
	}
	id := b.ID
	if id == "" {
		id = slug
	}
	adapter := b.Adapter
	if adapter == "" {
		adapter = "fse"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "schema: %s\nid: %s\n", schema, id)
	if b.Created != "" {
		fmt.Fprintf(&sb, "created: %s\n", b.Created)
	}
	fmt.Fprintf(&sb, "adapter: %s\n", adapter)
	if b.SourceTheme != "" {
		fmt.Fprintf(&sb, "source:\n  source_theme: %s\n", b.SourceTheme)
	}
	if b.Note != "" {
		fmt.Fprintf(&sb, "author:\n  note: %q\n", b.Note)
	}
	if b.Templates+b.Options+b.Attachments+b.UploadsFiles+b.UploadsBytes > 0 {
		fmt.Fprintln(&sb, "contents:")
		if b.Templates > 0 {
			fmt.Fprintf(&sb, "  templates_count: %d\n", b.Templates)
		}
		if b.Options > 0 {
			fmt.Fprintf(&sb, "  options_count: %d\n", b.Options)
		}
		if b.Attachments > 0 {
			fmt.Fprintf(&sb, "  attachments_count: %d\n", b.Attachments)
		}
		if b.UploadsFiles > 0 {
			fmt.Fprintf(&sb, "  uploads_file_count: %d\n", b.UploadsFiles)
		}
		if b.UploadsBytes > 0 {
			fmt.Fprintf(&sb, "  uploads_total_bytes: %d\n", b.UploadsBytes)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
