// Package manifest defines the fp.snapshot/v1 schema that the Go fp
// binary and the PHP-side wp fp snapshot command both produce /
// consume.
//
// The schema's canonical JSON Schema document is embedded as
// schema.json (see schema.go); the Go types here mirror that schema's
// shape. Whenever you change a field here, update schema.json AND
// the PHP-side FrankenPress\Cli\Snapshot\Capturer in lockstep.
//
// Schema versioning policy:
//
//   - fp.snapshot/v1   — current; backward-compatible additions OK
//   - fp.snapshot/v2   — created only when a backward-incompatible
//                        change is unavoidable
//
// `fp` refuses manifests with an unknown schema string up front.
package manifest

// SchemaV1 is the schema identifier this package writes and accepts.
const SchemaV1 = "fp.snapshot/v1"

// Manifest is the top-level fp.snapshot/v1 document.
type Manifest struct {
	Schema  string `json:"schema" yaml:"schema"`
	ID      string `json:"id" yaml:"id"`
	Created string `json:"created" yaml:"created"`

	Source        Source                  `json:"source" yaml:"source"`
	Author        Author                  `json:"author,omitempty" yaml:"author,omitempty"`
	AdaptersFired []string                `json:"adapters_fired" yaml:"adapters_fired"`
	Contents      Contents                `json:"contents" yaml:"contents"`
	AdapterState  map[string]AdapterState `json:"adapter_state,omitempty" yaml:"adapter_state,omitempty"`

	ComposerPatchSummary *ComposerPatchSummary `json:"composer_patch_summary,omitempty" yaml:"composer_patch_summary,omitempty"`
}

// Source captures the environment the snapshot was taken from.
type Source struct {
	SiteURL     string `json:"site_url" yaml:"site_url"`
	WPVersion   string `json:"wp_version" yaml:"wp_version"`
	ActiveTheme string `json:"active_theme" yaml:"active_theme"`
}

// Author records who took the snapshot. Free-form note for v1.
type Author struct {
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// Contents pins the snapshot artefacts. Relative filenames are
// resolved against the manifest's containing directory; the apply
// side uses db_sha256 to verify integrity before importing.
type Contents struct {
	DB                  string `json:"db" yaml:"db"`
	DBSHA256            string `json:"db_sha256" yaml:"db_sha256"`
	ComposerPatch       string `json:"composer_patch" yaml:"composer_patch"`
	UploadsManifest     string `json:"uploads_manifest" yaml:"uploads_manifest"`
	UploadsFileCount    int    `json:"uploads_file_count" yaml:"uploads_file_count"`
	UploadsTotalBytes   int64  `json:"uploads_total_bytes" yaml:"uploads_total_bytes"`
}

// AdapterState is an open-ended map of per-adapter capture data.
// Adapters (The7, Avada, Divi, ...) embed whatever they need to
// reconstruct theme-specific state on the apply side; the schema
// doesn't constrain the inner shape.
type AdapterState map[string]any

// ComposerPatchSummary lets the engineer eyeball the patch shape
// without opening composer-patch.json.
type ComposerPatchSummary struct {
	PendingCount    int `json:"pending_count" yaml:"pending_count"`
	UnresolvedCount int `json:"unresolved_count" yaml:"unresolved_count"`
}
