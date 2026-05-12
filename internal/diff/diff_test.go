package diff

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/summary"
)

func TestRead_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeSnap(t, dir, snapFixture{
		manifest: standardManifest("fse", "twentytwentyfive"),
		templates: `{
  "wp_template": {
    "header": {"post_title": "Header", "post_content": "<!-- wp:p /-->", "post_status": "publish", "post_excerpt": ""},
    "footer": {"post_title": "Footer", "post_content": "<!-- wp:f /-->", "post_status": "publish", "post_excerpt": ""}
  },
  "wp_navigation": {
    "navigation": {"post_title": "Nav", "post_content": "<!-- wp:page-list /-->", "post_status": "publish", "post_excerpt": ""}
  }
}`,
		options: `{
  "options": {
    "blogname": "Test Site",
    "site_logo": "32"
  },
  "theme_mods": {
    "twentytwentyfive": {
      "custom_css_post_id": -1
    }
  }
}`,
		attachments: `{
  "by_file": {
    "2026/05/logo.png": {"post_title": "Logo", "post_mime_type": "image/png"}
  }
}`,
		uploadsManifest: "abc123  100  2026/05/logo.png\ndef456  200  2026/05/footer.png\n",
	})

	s, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(s.Templates) != 3 {
		t.Errorf("len(Templates) = %d, want 3", len(s.Templates))
	}
	if s.Templates["wp_template/header"].PostTitle != "Header" {
		t.Errorf("templates[wp_template/header].PostTitle = %q", s.Templates["wp_template/header"].PostTitle)
	}
	if s.Options["blogname"] != "Test Site" {
		t.Errorf("Options[blogname] = %v", s.Options["blogname"])
	}
	if _, ok := s.ThemeMods["twentytwentyfive"]; !ok {
		t.Error("ThemeMods missing twentytwentyfive entry")
	}
	if _, ok := s.Attachments["2026/05/logo.png"]; !ok {
		t.Error("Attachments missing logo.png")
	}
	if u, ok := s.Uploads["2026/05/logo.png"]; !ok || u.Sha256 != "abc123" || u.Size != 100 {
		t.Errorf("Uploads[logo.png] = %+v", u)
	}
}

func TestRead_MissingOptionalSidecars(t *testing.T) {
	dir := t.TempDir()
	writeSnap(t, dir, snapFixture{manifest: standardManifest("fse", "twentytwentyfive")})

	s, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(s.Templates) != 0 || len(s.Options) != 0 || len(s.Attachments) != 0 || len(s.Uploads) != 0 {
		t.Errorf("expected all sidecars empty, got templates=%d options=%d att=%d uploads=%d",
			len(s.Templates), len(s.Options), len(s.Attachments), len(s.Uploads))
	}
}

func TestRead_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	_, err := Read(dir)
	if err == nil {
		t.Fatal("expected manifest-missing error")
	}
}

func TestCompare_Identical(t *testing.T) {
	a := makeSnap("a", "fse", "twentytwentyfive",
		map[string]Template{"wp_template/header": {PostType: "wp_template", PostName: "header", PostTitle: "Header", PostContent: "x"}},
		map[string]any{"blogname": "Site"},
		map[string]map[string]any{},
		map[string]Attachment{"img.png": {RelativeFile: "img.png"}},
		map[string]Upload{"img.png": {Sha256: "abc", Size: 100}},
	)
	b := makeSnap("b", "fse", "twentytwentyfive",
		map[string]Template{"wp_template/header": {PostType: "wp_template", PostName: "header", PostTitle: "Header", PostContent: "x"}},
		map[string]any{"blogname": "Site"},
		map[string]map[string]any{},
		map[string]Attachment{"img.png": {RelativeFile: "img.png"}},
		map[string]Upload{"img.png": {Sha256: "abc", Size: 100}},
	)
	res := Compare(a, b)
	if !res.Empty() {
		t.Errorf("Compare of identical snapshots is not Empty()\n%+v", res)
	}
}

func TestCompare_FullSpectrum(t *testing.T) {
	a := makeSnap("a", "fse", "twentytwentyfive",
		map[string]Template{
			"wp_template/header":   {PostType: "wp_template", PostName: "header", PostTitle: "Header", PostContent: "old"},
			"wp_template/old-page": {PostType: "wp_template", PostName: "old-page", PostTitle: "Old Page"},
		},
		map[string]any{"blogname": "Site", "old_key": "gone"},
		map[string]map[string]any{
			"twentytwentyfive": {"custom_css_post_id": float64(-1)},
		},
		map[string]Attachment{"logo.png": {RelativeFile: "logo.png"}},
		map[string]Upload{
			"logo.png":  {Sha256: "abc", Size: 100},
			"changed":   {Sha256: "111", Size: 50},
			"only-in-a": {Sha256: "xxx", Size: 10},
		},
	)
	b := makeSnap("b", "fse", "twentytwentyfive",
		map[string]Template{
			"wp_template/header":   {PostType: "wp_template", PostName: "header", PostTitle: "Header", PostContent: "new"},
			"wp_template/new-page": {PostType: "wp_template", PostName: "new-page", PostTitle: "New Page"},
		},
		map[string]any{"blogname": "Different Site", "new_key": "added"},
		map[string]map[string]any{
			"twentytwentyfive": {"custom_css_post_id": float64(42)},
		},
		map[string]Attachment{
			"logo.png":   {RelativeFile: "logo.png"},
			"footer.png": {RelativeFile: "footer.png"},
		},
		map[string]Upload{
			"logo.png":  {Sha256: "abc", Size: 100},
			"changed":   {Sha256: "222", Size: 60},
			"only-in-b": {Sha256: "yyy", Size: 20},
		},
	)
	res := Compare(a, b)

	if !contains(res.TemplatesAdded, "wp_template/new-page") {
		t.Errorf("TemplatesAdded missing new-page: %v", res.TemplatesAdded)
	}
	if !contains(res.TemplatesRemoved, "wp_template/old-page") {
		t.Errorf("TemplatesRemoved missing old-page: %v", res.TemplatesRemoved)
	}
	if len(res.TemplatesModified) != 1 || res.TemplatesModified[0].Key != "wp_template/header" {
		t.Errorf("TemplatesModified = %+v", res.TemplatesModified)
	}

	if !contains(res.OptionsAdded, "new_key") {
		t.Errorf("OptionsAdded missing new_key: %v", res.OptionsAdded)
	}
	if !contains(res.OptionsRemoved, "old_key") {
		t.Errorf("OptionsRemoved missing old_key: %v", res.OptionsRemoved)
	}
	foundBlogname := false
	for _, c := range res.OptionsChanged {
		if c.Key == "blogname" {
			foundBlogname = true
		}
	}
	if !foundBlogname {
		t.Errorf("OptionsChanged missing blogname: %+v", res.OptionsChanged)
	}

	if len(res.ThemeModsChanged) != 1 || res.ThemeModsChanged[0].Stylesheet != "twentytwentyfive" {
		t.Errorf("ThemeModsChanged = %+v", res.ThemeModsChanged)
	}

	if !contains(res.AttachmentsAdded, "footer.png") {
		t.Errorf("AttachmentsAdded missing footer.png: %v", res.AttachmentsAdded)
	}

	if !contains(res.UploadsAdded, "only-in-b") {
		t.Errorf("UploadsAdded missing only-in-b: %v", res.UploadsAdded)
	}
	if !contains(res.UploadsRemoved, "only-in-a") {
		t.Errorf("UploadsRemoved missing only-in-a: %v", res.UploadsRemoved)
	}
	foundChanged := false
	for _, c := range res.UploadsChanged {
		if c.Path == "changed" {
			foundChanged = true
			if c.ShaA != "111" || c.ShaB != "222" {
				t.Errorf("UploadsChanged shas: %+v", c)
			}
		}
	}
	if !foundChanged {
		t.Errorf("UploadsChanged missing 'changed': %+v", res.UploadsChanged)
	}
}

func TestCompare_ManifestFields(t *testing.T) {
	a := makeSnap("a", "fse", "twentytwentyfive", nil, nil, nil, nil, nil)
	b := makeSnap("b", "fse", "twentytwentyfour", nil, nil, nil, nil, nil)
	res := Compare(a, b)

	if len(res.ManifestChanges) != 1 {
		t.Fatalf("ManifestChanges = %d, want 1", len(res.ManifestChanges))
	}
	if res.ManifestChanges[0].Field != "source_theme" {
		t.Errorf("ManifestChanges[0].Field = %q", res.ManifestChanges[0].Field)
	}
}

func TestRender_EmptyResult(t *testing.T) {
	a := makeSnap("a", "fse", "twentytwentyfive", nil, nil, nil, nil, nil)
	b := makeSnap("b", "fse", "twentytwentyfive", nil, nil, nil, nil, nil)
	res := Compare(a, b)

	var out bytes.Buffer
	Render(&out, res)
	if !strings.Contains(out.String(), "no structural differences") {
		t.Errorf("expected no-diff sentinel:\n%s", out.String())
	}
}

func TestRender_FullSpectrum(t *testing.T) {
	a := makeSnap("a", "fse", "twentytwentyfive",
		map[string]Template{"wp_template/old": {PostName: "old"}},
		map[string]any{"site_logo": "32"},
		nil, nil, nil,
	)
	b := makeSnap("b", "fse", "twentytwentyfive",
		map[string]Template{"wp_template/new": {PostName: "new"}},
		map[string]any{"site_logo": "47"},
		nil, nil, nil,
	)
	res := Compare(a, b)

	var out bytes.Buffer
	Render(&out, res)
	for _, want := range []string{
		"templates",
		"- wp_template/old",
		"+ wp_template/new",
		"options",
		"~ site_logo: \"32\" -> \"47\"",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q\n%s", want, out.String())
		}
	}
}

// --- helpers ---

type snapFixture struct {
	manifest        string
	templates       string
	options         string
	attachments     string
	uploadsManifest string
}

func writeSnap(t *testing.T, dir string, f snapFixture) {
	t.Helper()
	must := func(p string, b []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, p), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("manifest.yaml", []byte(f.manifest))
	if f.templates != "" {
		must("templates.json", []byte(f.templates))
	}
	if f.options != "" {
		must("options.json", []byte(f.options))
	}
	if f.attachments != "" {
		must("attachments.json", []byte(f.attachments))
	}
	if f.uploadsManifest != "" {
		must("uploads-manifest.txt", []byte(f.uploadsManifest))
	}
}

func standardManifest(adapter, theme string) string {
	return `schema: fp.snapshot/v4
id: test
adapter: ` + adapter + `
source:
  source_theme: ` + theme + `
author:
  note: ""
contents:
  templates_count: 0
  options_count: 0
  attachments_count: 0
`
}

func makeSnap(name, adapter, theme string,
	templates map[string]Template,
	options map[string]any,
	themeMods map[string]map[string]any,
	attachments map[string]Attachment,
	uploads map[string]Upload,
) *Snapshot {
	if templates == nil {
		templates = map[string]Template{}
	}
	if options == nil {
		options = map[string]any{}
	}
	if themeMods == nil {
		themeMods = map[string]map[string]any{}
	}
	if attachments == nil {
		attachments = map[string]Attachment{}
	}
	if uploads == nil {
		uploads = map[string]Upload{}
	}
	return &Snapshot{
		Path: "/tmp/" + name,
		Manifest: &summary.Manifest{
			Schema:  "fp.snapshot/v4",
			Adapter: adapter,
			Source:  summary.Source{SourceTheme: theme},
		},
		Templates:   templates,
		Options:     options,
		ThemeMods:   themeMods,
		Attachments: attachments,
		Uploads:     uploads,
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
