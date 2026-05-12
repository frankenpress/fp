// Package state persists per-repo CLI state under .fp/state.json.
//
// State is repo-local rather than per-user — designers working on
// multiple FrankenPress sites should not share a "last_slug" between
// them. The file is gitignored (added to .fp/.gitignore by Save the
// first time it writes) but stays on the designer's filesystem so
// the slug-default cascade has something to suggest on the second
// invocation.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	dirName  = ".fp"
	fileName = "state.json"
)

// Version is the schema version stamped into every freshly-written
// state file. Bump only on shape-breaking changes; Load tolerates
// unknown versions and reads what it can.
const Version = 1

// State is the on-disk shape of .fp/state.json. Fields are JSON-tagged
// in snake_case for readability when designers `cat` the file.
type State struct {
	Version       int       `json:"version"`
	LastSlug      string    `json:"last_slug,omitempty"`
	LastNoteUsed  string    `json:"last_note_used,omitempty"`
	LastCaptureAt time.Time `json:"last_capture_at,omitempty"`
}

// Path returns the absolute state.json path for the given repo root.
func Path(repoRoot string) string {
	return filepath.Join(repoRoot, dirName, fileName)
}

// Load reads .fp/state.json from repoRoot. A missing file returns an
// empty State with no error — first-time invocations are normal.
func Load(repoRoot string) (*State, error) {
	data, err := os.ReadFile(Path(repoRoot))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{Version: Version}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Version == 0 {
		s.Version = Version
	}
	return &s, nil
}

// Save writes the state file under .fp/, creating the directory if
// missing and dropping a .fp/.gitignore that ignores the directory's
// contents (so designers don't accidentally commit per-machine slugs).
func Save(repoRoot string, s *State) error {
	if s == nil {
		return errors.New("state: Save called with nil state")
	}
	if s.Version == 0 {
		s.Version = Version
	}

	dir := filepath.Join(repoRoot, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(gi, []byte("# fp CLI per-repo state — keep on this machine, do not commit\n*\n!.gitignore\n"), 0o644); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Write atomically via a sibling tempfile + rename so a crashed
	// fp can't leave a half-written state.json.
	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), Path(repoRoot))
}
