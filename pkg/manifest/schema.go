package manifest

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed schema.json
var SchemaJSON []byte

// SchemaDocument unmarshals the embedded JSON Schema into a generic
// map. Tools that want to do their own validation can consume it.
func SchemaDocument() (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(SchemaJSON, &doc); err != nil {
		return nil, fmt.Errorf("manifest: embedded schema.json is malformed: %w", err)
	}
	return doc, nil
}

// ErrUnsupportedSchema is returned by Parse when a manifest's schema
// field is not one the binary knows. Callers can check via
// errors.Is(err, ErrUnsupportedSchema) and surface a "please upgrade
// `fp` to ≥vX.Y.Z" message.
var ErrUnsupportedSchema = errors.New("manifest: unsupported schema version")

// Parse decodes raw JSON manifest bytes into a Manifest and validates
// the schema field is one this build accepts.
//
// Returns an explicit migration error for the now-deprecated v1
// schema — fp v0.4.0+ refuses v1 manifests because they predate the
// adapter-scope safety boundary.
func Parse(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest: invalid JSON: %w", err)
	}
	switch m.Schema {
	case SchemaV2:
		return &m, nil
	case SchemaV1:
		return nil, fmt.Errorf("%w: v1 manifests are no longer accepted; v1 used a full-replace apply path that could destroy live-site data. Re-capture with mu-plugin v0.8.0+ (which emits v2 — adapter-scoped, additive)", ErrUnsupportedSchema)
	default:
		return nil, fmt.Errorf("%w: got %q, this fp accepts %q", ErrUnsupportedSchema, m.Schema, SchemaV2)
	}
}
