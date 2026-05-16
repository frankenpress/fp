package aws

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Fake is a recording Runner for tests. Pre-populate Prefixes / Files
// for canned responses; invocations are recorded on Calls.
type Fake struct {
	Calls []Call

	// Prefixes is returned by ListSnapshotPrefixes. Tests can leave it
	// nil to simulate an empty bucket.
	Prefixes    []string
	ListErr     error
	SyncDownErr error

	// SyncFiles is a map of slug → relative file path → file content.
	// When SyncDown is invoked for a slug present here, the fake
	// materialises those files under destDir (mirroring what aws s3 sync
	// would do). When the slug isn't in the map, SyncDown is a no-op
	// (creates destDir only). Tests can leave it nil for pure call-
	// counting work.
	SyncFiles map[string]map[string][]byte
}

// Call records a single Runner invocation.
type Call struct {
	Method  string
	Bucket  string
	Slug    string
	DestDir string
	Profile string
	Region  string
}

// NewFake returns an initialised Fake.
func NewFake() *Fake {
	return &Fake{SyncFiles: map[string]map[string][]byte{}}
}

func (f *Fake) ListSnapshotPrefixes(_ context.Context, bucket, profile, region string) ([]string, error) {
	f.Calls = append(f.Calls, Call{
		Method:  "ListSnapshotPrefixes",
		Bucket:  bucket,
		Profile: profile,
		Region:  region,
	})
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return append([]string(nil), f.Prefixes...), nil
}

func (f *Fake) SyncDown(_ context.Context, bucket, slug, destDir, profile, region string) error {
	f.Calls = append(f.Calls, Call{
		Method:  "SyncDown",
		Bucket:  bucket,
		Slug:    slug,
		DestDir: destDir,
		Profile: profile,
		Region:  region,
	})
	if f.SyncDownErr != nil {
		return f.SyncDownErr
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	files, ok := f.SyncFiles[slug]
	if !ok {
		return nil
	}
	for rel, content := range files {
		full := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CallCount returns how many times the named method was invoked.
func (f *Fake) CallCount(method string) int {
	n := 0
	for _, c := range f.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// ErrFake is a sentinel for tests that want a generic aws failure.
var ErrFake = errors.New("fake aws error")
