package manifest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSchemaDocumentParses(t *testing.T) {
	doc, err := SchemaDocument()
	if err != nil {
		t.Fatalf("SchemaDocument: %v", err)
	}
	if id, ok := doc["$id"].(string); !ok || id == "" {
		t.Fatalf("schema $id missing or not a string; got %T %v", doc["$id"], doc["$id"])
	}
}

func TestParseAcceptsCurrentSchema(t *testing.T) {
	raw := []byte(`{
		"schema": "fp.snapshot/v2",
		"id": "architect-2-20260511-091422",
		"created": "2026-05-11T09:14:22Z",
		"source": {
			"site_url": "http://localhost:8080",
			"wp_version": "6.8.5",
			"active_theme": "dt-the7"
		},
		"adapters_fired": ["the7"],
		"scope": {
			"post_types_with_marker": {"page": "_the7_imported_item"},
			"post_types_full_capture": ["nav_menu_item"],
			"option_patterns": ["the7_%", "elementor_%"],
			"theme_mods_for": ["dt-the7"],
			"documented_exclusions": ["wc_orders", "wp_users", "wp_comments"]
		},
		"contents": {
			"wxr": "content.xml.gz",
			"wxr_sha256": "3a7f000000000000000000000000000000000000000000000000000000000000",
			"wxr_post_count": 42,
			"options": "options.json",
			"options_sha256": "bbbb000000000000000000000000000000000000000000000000000000000000",
			"options_count": 17,
			"composer_patch": "composer-patch.json",
			"uploads_manifest": "uploads-manifest.txt",
			"uploads_file_count": 412,
			"uploads_total_bytes": 2400000000
		}
	}`)

	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Schema != SchemaV2 {
		t.Fatalf("Schema = %q, want %q", m.Schema, SchemaV2)
	}
	if m.ID != "architect-2-20260511-091422" {
		t.Fatalf("ID = %q", m.ID)
	}
	if len(m.AdaptersFired) != 1 || m.AdaptersFired[0] != "the7" {
		t.Fatalf("AdaptersFired = %v", m.AdaptersFired)
	}
	if m.Scope.PostTypesWithMarker["page"] != "_the7_imported_item" {
		t.Fatalf("Scope.PostTypesWithMarker[page] = %q", m.Scope.PostTypesWithMarker["page"])
	}
	if len(m.Scope.DocumentedExclusions) != 3 {
		t.Fatalf("Scope.DocumentedExclusions len = %d", len(m.Scope.DocumentedExclusions))
	}
	if m.Contents.WXRPostCount != 42 {
		t.Fatalf("Contents.WXRPostCount = %d", m.Contents.WXRPostCount)
	}
	if m.Contents.OptionsCount != 17 {
		t.Fatalf("Contents.OptionsCount = %d", m.Contents.OptionsCount)
	}
}

func TestParseRejectsV1WithMigrationHint(t *testing.T) {
	raw := []byte(`{"schema": "fp.snapshot/v1", "id": "x"}`)
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected error for v1 schema, got nil")
	}
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected errors.Is ErrUnsupportedSchema; got %v", err)
	}
	if !strings.Contains(err.Error(), "v1 manifests are no longer accepted") {
		t.Errorf("error should explicitly call out v1 migration; got %v", err)
	}
	if !strings.Contains(err.Error(), "mu-plugin v0.8.0") {
		t.Errorf("error should point at the migration target; got %v", err)
	}
}

func TestParseRejectsUnknownSchema(t *testing.T) {
	raw := []byte(`{"schema": "fp.snapshot/v9", "id": "x"}`)
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected errors.Is ErrUnsupportedSchema; got %v", err)
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	_, err := Parse([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestManifestRoundTrip(t *testing.T) {
	m := &Manifest{
		Schema:        SchemaV2,
		ID:            "test",
		Created:       "2026-05-11T00:00:00Z",
		AdaptersFired: []string{"the7"},
		Source: Source{
			SiteURL:     "http://localhost:8080",
			WPVersion:   "6.8.5",
			ActiveTheme: "dt-the7",
		},
		Scope: Scope{
			PostTypesWithMarker:  map[string]string{"page": "_the7_imported_item"},
			PostTypesFullCapture: []string{"nav_menu_item"},
			OptionPatterns:       []string{"the7_%"},
			ThemeModsFor:         []string{"dt-the7"},
			DocumentedExclusions: []string{"wc_orders"},
		},
		Contents: Contents{
			WXR:             "content.xml.gz",
			WXRSHA256:       "0000000000000000000000000000000000000000000000000000000000000000",
			Options:         "options.json",
			OptionsSHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
			ComposerPatch:   "composer-patch.json",
			UploadsManifest: "uploads-manifest.txt",
		},
	}

	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.ID != m.ID {
		t.Fatalf("round-trip ID mismatch: %q vs %q", parsed.ID, m.ID)
	}
	if parsed.Scope.PostTypesWithMarker["page"] != "_the7_imported_item" {
		t.Fatal("round-trip scope mismatch")
	}
}
