package promote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvValid(t *testing.T) {
	tests := []struct {
		env  Env
		want bool
	}{
		{EnvStaging, true},
		{EnvProduction, true},
		{Env(""), false},
		{Env("staging"), false},
		{Env("PROD"), false},
		{Env("dev"), false},
	}
	for _, tc := range tests {
		if got := tc.env.Valid(); got != tc.want {
			t.Errorf("Env(%q).Valid() = %v, want %v", string(tc.env), got, tc.want)
		}
	}
}

func TestSnapshotArtefactsIsStable(t *testing.T) {
	a := SnapshotArtefacts()
	b := SnapshotArtefacts()
	if len(a) == 0 {
		t.Fatal("expected non-empty artefact list")
	}
	if len(a) != len(b) {
		t.Fatalf("artefact list not stable: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("artefact list not stable at index %d: %q vs %q", i, a[i], b[i])
		}
	}
	// db.sql.gz must be present — the apply path absolutely needs it.
	found := false
	for _, name := range a {
		if name == "db.sql.gz" {
			found = true
		}
	}
	if !found {
		t.Error("db.sql.gz missing from SnapshotArtefacts()")
	}
}

func TestLoadManifestRejectsMissingDir(t *testing.T) {
	_, err := LoadManifest("/nope/does/not/exist")
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestLoadManifestRejectsBadSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"schema":"fp.snapshot/v999","id":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected unsupported-schema error")
	}
	if !strings.Contains(err.Error(), "unsupported schema") {
		t.Errorf("error should mention unsupported schema; got %v", err)
	}
}

func TestGitopsPRRendersBodyShape(t *testing.T) {
	p := GitopsPR{
		GitopsRepo:     "aypex-io/gitops-fp",
		Applicationset: "apps/applicationset.yaml",
		SiteKey:        "sts",
		Bucket:         "sts-snapshots",
	}
	body := p.renderBody(EnvStaging, "architect-2-20260511-091422", "snapshots/architect-2-20260511-091422/")

	for _, needle := range []string{
		"snapshot:",
		"architect-2-20260511-091422",
		"snapshots/architect-2-20260511-091422/",
		"sts-snapshots",
		"apps/applicationset.yaml",
		"fp_snapshot_applied_ref",
		"site: sts",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("rendered body missing %q\n--- body ---\n%s", needle, body)
		}
	}
}

func TestUploadRejectsMissingArtefacts(t *testing.T) {
	dir := t.TempDir() // empty
	u := Uploader{Bucket: "test"}
	_, err := u.Upload(t.Context(), dir, "test-id")
	if err == nil {
		t.Fatal("expected error for empty snapshot dir")
	}
	if !strings.Contains(err.Error(), "missing snapshot artefact") {
		t.Errorf("error should mention missing artefact; got %v", err)
	}
}

func TestUploadRejectsEmptyBucket(t *testing.T) {
	u := Uploader{Bucket: ""}
	_, err := u.Upload(t.Context(), t.TempDir(), "x")
	if err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

func TestUploadRejectsEmptyID(t *testing.T) {
	u := Uploader{Bucket: "b"}
	_, err := u.Upload(t.Context(), t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty snapshot id")
	}
}
