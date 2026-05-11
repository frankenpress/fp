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
// map. Tools that want to do their own validation (a future
// `fp doctor` schema check, IDE integrations) can consume it.
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
// the schema field is one this build accepts. Deeper field-level
// validation against schema.json is intentionally not done here —
// Phase 2 of the CLI plan adds a santhosh-tekuri/jsonschema pass for
// that, when promote starts shipping the manifest to untrusted
// destinations.
func Parse(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest: invalid JSON: %w", err)
	}
	if m.Schema != SchemaV1 {
		return nil, fmt.Errorf("%w: got %q, this fp accepts %q", ErrUnsupportedSchema, m.Schema, SchemaV1)
	}
	return &m, nil
}
