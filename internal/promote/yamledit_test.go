package promote

import (
	"errors"
	"strings"
	"testing"
)

const sampleAppSet = `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: sites
spec:
  generators:
    - matrix:
        generators:
          - list:
              elements:
                - site: eoe
                  imageTagStg: v0.1.10
                  imageTagPrd: v0.1.10
                - site: sts
                  host: soletradersupport.co.uk
                  imageTagStg: v0.0.5
                  imageTagPrd: v0.0.4
                  activeTheme: dt-the7
          - list:
              elements:
                - env: stg
                - env: prd
  template:
    metadata:
      name: '{{ .site }}-{{ .env }}'
`

func TestEditApplicationSet_AddsSnapshotBlockWhenAbsent(t *testing.T) {
	out, err := EditApplicationSet([]byte(sampleAppSet), "sts", SnapshotValues{
		Ref:    "architect-2-20260511-091422",
		S3Key:  "snapshots/architect-2-20260511-091422/",
		Bucket: "sts-snapshots",
	})
	if err != nil {
		t.Fatalf("EditApplicationSet: %v", err)
	}
	s := string(out)
	for _, needle := range []string{
		"site: sts",
		"snapshot:",
		"ref: architect-2-20260511-091422",
		"s3Key: snapshots/architect-2-20260511-091422/",
		"bucket: sts-snapshots",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("output missing %q\n--- got ---\n%s", needle, s)
		}
	}
	// Sanity: eoe entry should be unchanged (no snapshot block added).
	if strings.Count(s, "snapshot:") != 1 {
		t.Errorf("expected exactly one snapshot block, got %d:\n%s", strings.Count(s, "snapshot:"), s)
	}
}

func TestEditApplicationSet_UpdatesExistingSnapshotBlock(t *testing.T) {
	withSnapshot := sampleAppSet + `                  snapshot:
                    ref: old-snapshot
                    s3Key: snapshots/old/
                    bucket: sts-snapshots
`
	// Embed the snapshot block under sts. Hand-construct so test reads
	// naturally:
	preEdit := strings.Replace(sampleAppSet,
		"activeTheme: dt-the7",
		"activeTheme: dt-the7\n                  snapshot:\n                    ref: old-snap\n                    s3Key: snapshots/old/\n                    bucket: sts-snapshots",
		1)
	_ = withSnapshot // alternate hand-rolled version, unused but kept for reference

	out, err := EditApplicationSet([]byte(preEdit), "sts", SnapshotValues{
		Ref:    "new-snap",
		S3Key:  "snapshots/new/",
		Bucket: "sts-snapshots",
	})
	if err != nil {
		t.Fatalf("EditApplicationSet: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "old-snap") {
		t.Errorf("old ref still present:\n%s", s)
	}
	if !strings.Contains(s, "ref: new-snap") {
		t.Errorf("new ref missing:\n%s", s)
	}
	if strings.Count(s, "snapshot:") != 1 {
		t.Errorf("expected exactly one snapshot block, got %d", strings.Count(s, "snapshot:"))
	}
}

func TestEditApplicationSet_ErrorsOnMissingSite(t *testing.T) {
	_, err := EditApplicationSet([]byte(sampleAppSet), "nonexistent", SnapshotValues{
		Ref: "x", S3Key: "y", Bucket: "z",
	})
	if err == nil {
		t.Fatal("expected error for missing site")
	}
	if !errors.Is(err, ErrSiteEntryNotFound) {
		t.Fatalf("expected ErrSiteEntryNotFound; got %v", err)
	}
}

func TestEditApplicationSet_ErrorsOnEmptySiteKey(t *testing.T) {
	_, err := EditApplicationSet([]byte(sampleAppSet), "", SnapshotValues{})
	if err == nil {
		t.Fatal("expected error for empty siteKey")
	}
}

func TestEditApplicationSet_ErrorsOnInvalidYAML(t *testing.T) {
	_, err := EditApplicationSet([]byte("not: valid: yaml: at: all: ::::"), "sts", SnapshotValues{})
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestEditApplicationSet_PreservesOtherFields(t *testing.T) {
	out, err := EditApplicationSet([]byte(sampleAppSet), "sts", SnapshotValues{
		Ref: "x", S3Key: "y", Bucket: "z",
	})
	if err != nil {
		t.Fatalf("EditApplicationSet: %v", err)
	}
	s := string(out)
	for _, needle := range []string{
		"host: soletradersupport.co.uk",
		"imageTagStg: v0.0.5",
		"imageTagPrd: v0.0.4",
		"activeTheme: dt-the7",
		"site: eoe",
		"env: stg",
		"env: prd",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("preserved-field check failed for %q", needle)
		}
	}
}
