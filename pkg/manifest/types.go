// Package manifest defines the fp.snapshot/v2 schema that the Go fp
// binary and the PHP-side wp fp snapshot command both produce and
// consume.
//
// The schema's canonical JSON Schema document is embedded as
// schema.json (see schema.go); the Go types here mirror that schema's
// shape. Whenever you change a field here, update schema.json AND
// the PHP-side FrankenPress\Cli\Snapshot\Capturer in lockstep.
//
// Schema versioning policy:
//
//   - fp.snapshot/v1 — DEPRECATED. SQL-dump-based, full-replace
//     apply path. Removed in fp v0.4.0 / mu-plugin
//     v0.8.0 / charts v0.9.0 (the coordinated
//     WXR/adapter-scoped/additive rewrite).
//   - fp.snapshot/v2 — current. WXR + scoped options. Additive apply.
//     Backward-compatible additions OK within v2.
//   - fp.snapshot/v3 — created only if a backward-incompatible
//     change is unavoidable.
//
// `fp` refuses manifests with an unknown schema string up front.
package manifest

// SchemaV2 is the schema identifier this package writes and accepts.
const SchemaV2 = "fp.snapshot/v2"

// SchemaV1 is recognised only to surface a helpful migration error
// for anyone with a v1 manifest. fp v0.4.0+ refuses to parse it.
const SchemaV1 = "fp.snapshot/v1"

// Manifest is the top-level fp.snapshot/v2 document.
type Manifest struct {
	Schema  string `json:"schema" yaml:"schema"`
	ID      string `json:"id" yaml:"id"`
	Created string `json:"created" yaml:"created"`

	Source        Source                  `json:"source" yaml:"source"`
	Author        Author                  `json:"author,omitempty" yaml:"author,omitempty"`
	AdaptersFired []string                `json:"adapters_fired" yaml:"adapters_fired"`
	Scope         Scope                   `json:"scope" yaml:"scope"`
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

// Author records who took the snapshot. Free-form note for v2.
type Author struct {
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// Scope is the declarative blast radius of a snapshot — the union of
// every fired adapter's SnapshotScope. Surfaced into the manifest so
// a reviewing engineer can see exactly which tables / option patterns
// the snapshot's apply path will touch, and (via documented_exclusions)
// what it explicitly cannot touch.
type Scope struct {
	PostTypesWithMarker  map[string]string `json:"post_types_with_marker" yaml:"post_types_with_marker"`
	PostTypesFullCapture []string          `json:"post_types_full_capture" yaml:"post_types_full_capture"`
	OptionPatterns       []string          `json:"option_patterns" yaml:"option_patterns"`
	ThemeModsFor         []string          `json:"theme_mods_for" yaml:"theme_mods_for"`
	DocumentedExclusions []string          `json:"documented_exclusions" yaml:"documented_exclusions"`
}

// Contents pins the snapshot artefacts. Relative filenames are
// resolved against the manifest's containing directory; the apply
// side uses wxr_sha256 and options_sha256 to verify integrity before
// applying.
type Contents struct {
	WXR               string `json:"wxr" yaml:"wxr"`
	WXRSHA256         string `json:"wxr_sha256" yaml:"wxr_sha256"`
	WXRPostCount      int    `json:"wxr_post_count" yaml:"wxr_post_count"`
	Options           string `json:"options" yaml:"options"`
	OptionsSHA256     string `json:"options_sha256" yaml:"options_sha256"`
	OptionsCount      int    `json:"options_count" yaml:"options_count"`
	ComposerPatch     string `json:"composer_patch" yaml:"composer_patch"`
	UploadsManifest   string `json:"uploads_manifest" yaml:"uploads_manifest"`
	UploadsFileCount  int    `json:"uploads_file_count" yaml:"uploads_file_count"`
	UploadsTotalBytes int64  `json:"uploads_total_bytes" yaml:"uploads_total_bytes"`
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
