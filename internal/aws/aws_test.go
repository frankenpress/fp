package aws

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParsePrefixList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "happy path with three prefixes out of order",
			in: "                           PRE prod-2026-05-16T00-00-00Z/\n" +
				"                           PRE prod-2026-05-14T00-00-00Z/\n" +
				"                           PRE prod-2026-05-15T00-00-00Z/\n",
			want: []string{
				"prod-2026-05-14T00-00-00Z",
				"prod-2026-05-15T00-00-00Z",
				"prod-2026-05-16T00-00-00Z",
			},
		},
		{
			name: "ignores object lines (only PRE lines count)",
			in: "2026-05-16 00:00:01        128 some-stray-file.txt\n" +
				"                           PRE prod-2026-05-16T00-00-00Z/\n",
			want: []string{"prod-2026-05-16T00-00-00Z"},
		},
		{
			name: "empty input → empty slice",
			in:   "",
			want: []string{},
		},
		{
			name: "no PRE lines → empty slice",
			in:   "2026-05-16 00:00:01        128 some-file.txt\n",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePrefixList([]byte(tt.in))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePrefixList = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFakeSyncDownMaterialisesFiles(t *testing.T) {
	tmp := t.TempDir()
	fake := NewFake()
	fake.SyncFiles = map[string]map[string][]byte{
		"prod-2026-05-16T00-00-00Z": {
			"manifest.yaml":    []byte("schema: fp.snapshot/v5\nid: prod-2026-05-16T00-00-00Z\n"),
			"content.xml.gz":   []byte{0x1f, 0x8b, 0x08},
			"uploads/foo.json": []byte("{}"),
		},
	}

	dest := filepath.Join(tmp, "out")
	err := fake.SyncDown(context.Background(), "test-bucket", "prod-2026-05-16T00-00-00Z", dest, "", "")
	if err != nil {
		t.Fatalf("SyncDown: %v", err)
	}

	for rel, want := range fake.SyncFiles["prod-2026-05-16T00-00-00Z"] {
		got, err := os.ReadFile(filepath.Join(dest, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Errorf("content mismatch for %s", rel)
		}
	}

	if fake.CallCount("SyncDown") != 1 {
		t.Errorf("CallCount(SyncDown) = %d, want 1", fake.CallCount("SyncDown"))
	}
}

func TestFakeListSnapshotPrefixesReturnsCopy(t *testing.T) {
	fake := NewFake()
	fake.Prefixes = []string{"prod-2026-05-14T00-00-00Z", "prod-2026-05-15T00-00-00Z"}

	got, err := fake.ListSnapshotPrefixes(context.Background(), "test-bucket", "mkennedy", "eu-west-2")
	if err != nil {
		t.Fatalf("ListSnapshotPrefixes: %v", err)
	}
	if !reflect.DeepEqual(got, fake.Prefixes) {
		t.Errorf("got %v, want %v", got, fake.Prefixes)
	}

	// Mutating the returned slice must not poison Fake.Prefixes.
	got[0] = "mutated"
	if fake.Prefixes[0] == "mutated" {
		t.Error("returned slice shares storage with Fake.Prefixes")
	}

	if got, want := fake.Calls[0].Profile, "mkennedy"; got != want {
		t.Errorf("profile recorded = %q, want %q", got, want)
	}
	if got, want := fake.Calls[0].Region, "eu-west-2"; got != want {
		t.Errorf("region recorded = %q, want %q", got, want)
	}
}

func TestFakeReturnsErrorWhenSet(t *testing.T) {
	fake := NewFake()
	fake.ListErr = ErrFake
	_, err := fake.ListSnapshotPrefixes(context.Background(), "x", "", "")
	if err != ErrFake {
		t.Errorf("got %v, want ErrFake", err)
	}
}

func TestCommonArgsOmitsEmpty(t *testing.T) {
	tests := []struct {
		profile, region string
		want            []string
	}{
		{"", "", []string{}},
		{"mkennedy", "", []string{"--profile", "mkennedy"}},
		{"", "eu-west-2", []string{"--region", "eu-west-2"}},
		{"mkennedy", "eu-west-2", []string{"--profile", "mkennedy", "--region", "eu-west-2"}},
	}
	for _, tt := range tests {
		got := commonArgs(tt.profile, tt.region)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("commonArgs(%q,%q) = %v, want %v", tt.profile, tt.region, got, tt.want)
		}
	}
}
