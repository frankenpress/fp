package manifest

import (
	"encoding/json"
	"errors"
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
		"schema": "fp.snapshot/v1",
		"id": "architect-2-20260511-091422",
		"created": "2026-05-11T09:14:22Z",
		"source": {
			"site_url": "http://localhost:8080",
			"wp_version": "6.8.5",
			"active_theme": "dt-the7"
		},
		"adapters_fired": ["the7"],
		"contents": {
			"db": "db.sql.gz",
			"db_sha256": "3a7f000000000000000000000000000000000000000000000000000000000000",
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
	if m.Schema != SchemaV1 {
		t.Fatalf("Schema = %q, want %q", m.Schema, SchemaV1)
	}
	if m.ID != "architect-2-20260511-091422" {
		t.Fatalf("ID = %q", m.ID)
	}
	if len(m.AdaptersFired) != 1 || m.AdaptersFired[0] != "the7" {
		t.Fatalf("AdaptersFired = %v", m.AdaptersFired)
	}
	if m.Contents.UploadsFileCount != 412 {
		t.Fatalf("UploadsFileCount = %d", m.Contents.UploadsFileCount)
	}
}

func TestParseRejectsUnknownSchema(t *testing.T) {
	raw := []byte(`{"schema": "fp.snapshot/v2", "id": "x"}`)
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected errors.Is(err, ErrUnsupportedSchema); got %v", err)
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
		Schema:        SchemaV1,
		ID:            "test",
		Created:       "2026-05-11T00:00:00Z",
		AdaptersFired: []string{"the7"},
		Source: Source{
			SiteURL:     "http://localhost:8080",
			WPVersion:   "6.8.5",
			ActiveTheme: "dt-the7",
		},
		Contents: Contents{
			DB:              "db.sql.gz",
			DBSHA256:        "0000000000000000000000000000000000000000000000000000000000000000",
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
}
