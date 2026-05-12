package summary

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrint_RendersExpectedFields(t *testing.T) {
	m := &Manifest{
		Schema:   "fp.snapshot/v4",
		Adapter:  "fse",
		Source:   Source{SourceTheme: "twentytwentyfive"},
		Author:   Author{Note: "Footer image refresh"},
		Scope:    Scope{PostTypesOwned: []string{"wp_template", "wp_template_part"}},
		Contents: Counts{TemplatesCount: 5, OptionsCount: 8, AttachmentsCount: 2, BinariesTotalBytes: 144507, UploadsFileCount: 16, UploadsTotalBytes: 1000505},
	}
	var out bytes.Buffer
	Print(&out, m, "sts-launch", "web/imports/sts-launch")
	s := out.String()
	for _, want := range []string{
		"captured snapshot: sts-launch",
		"schema:          fp.snapshot/v4",
		"adapter:         fse",
		"source theme:    twentytwentyfive",
		"templates:       5 (wp_template + wp_template_part)",
		"options:         8",
		"attachments:     2 -> 141.1 KB binaries",
		"uploads audit:   16 files, 977.1 KB",
		"git add web/imports/sts-launch/",
		"Footer image refresh",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q\n--- full output ---\n%s", want, s)
		}
	}
}

func TestPrint_NewerSchemaPrintsWarning(t *testing.T) {
	m := &Manifest{Schema: "fp.snapshot/v99"}
	var out bytes.Buffer
	Print(&out, m, "x", "web/imports/x")
	if !strings.Contains(out.String(), "newer than this fp build understands") {
		t.Errorf("expected newer-schema warning\n%s", out.String())
	}
}

func TestRead_ParsesRealManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(`schema: fp.snapshot/v4
id: x
adapter: fse
source:
  source_theme: twentytwentyfive
author:
  note: hello
contents:
  templates_count: 7
`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if m.Adapter != "fse" {
		t.Errorf("Adapter = %q", m.Adapter)
	}
	if m.Source.SourceTheme != "twentytwentyfive" {
		t.Errorf("SourceTheme = %q", m.Source.SourceTheme)
	}
	if m.Contents.TemplatesCount != 7 {
		t.Errorf("TemplatesCount = %d", m.Contents.TemplatesCount)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
	}
	for _, tc := range cases {
		got := humanBytes(tc.in)
		if got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
