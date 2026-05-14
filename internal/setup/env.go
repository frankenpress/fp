// Package setup contains the fp init orchestration: detect fresh-
// clone state, run composer install + .env scaffolding, bring the
// docker-compose stack up, install WordPress if needed, and apply
// the latest snapshot.
//
// Pure helpers (env file IO, fresh-clone detection) live alongside
// the orchestrator; testability follows the docker.Runner /
// gh.Runner Fake pattern for any shell-out.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadEnvKey returns the value of `key` from the .env at envPath if
// an uncommented `key=...` assignment is present. found=false when
// the key is absent or only appears in a commented documentation line.
// Quotes are NOT stripped (designers typically don't quote values in
// site-template's .env.example).
func ReadEnvKey(envPath, key string) (value string, found bool, err error) {
	if key == "" {
		return "", false, errors.New("ReadEnvKey: key is required")
	}
	f, err := os.Open(envPath)
	if err != nil {
		return "", false, fmt.Errorf("open %s: %w", envPath, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	prefix := key + "="
	for scanner.Scan() {
		line := strings.TrimLeft(scanner.Text(), " \t")
		if strings.HasPrefix(line, prefix) {
			return line[len(prefix):], true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("scan %s: %w", envPath, err)
	}
	return "", false, nil
}

// EnvFileMissing reports whether the .env at envPath does not exist.
// Any other stat error propagates (permissions, IO).
func EnvFileMissing(envPath string) (bool, error) {
	_, err := os.Stat(envPath)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

// ScaffoldEnvFromExample copies .env.example → .env when .env does
// not yet exist. Idempotent: returns (false, nil) when .env already
// exists, (true, nil) after a successful copy.
//
// The repo's .env.example is the source of truth for default values;
// fp init doesn't second-guess what the template author put there.
// Subsequent passes (EnsureEnvKey) layer designer-mode overrides on
// top of whatever the example provided.
func ScaffoldEnvFromExample(repoRoot string) (created bool, err error) {
	envPath := filepath.Join(repoRoot, ".env")
	examplePath := filepath.Join(repoRoot, ".env.example")

	missing, err := EnvFileMissing(envPath)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", envPath, err)
	}
	if !missing {
		return false, nil
	}

	src, err := os.Open(examplePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf(".env.example not found at %s — is this a site-template-shaped repo?", examplePath)
		}
		return false, fmt.Errorf("open %s: %w", examplePath, err)
	}
	defer src.Close()

	// Create with O_EXCL to refuse to overwrite if the file appeared
	// between our stat and create (a benign race, but it'd be wrong
	// to clobber an operator's just-written .env).
	dst, err := os.OpenFile(envPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil // someone wrote it between our stat + create, fine
		}
		return false, fmt.Errorf("create %s: %w", envPath, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return false, fmt.Errorf("copy %s → %s: %w", examplePath, envPath, err)
	}
	if err := dst.Close(); err != nil {
		return false, fmt.Errorf("close %s: %w", envPath, err)
	}
	return true, nil
}

// EnsureEnvKey idempotently adds `key=value` to the .env at envPath
// when no uncommented `key=...` line exists. Returns (true, nil)
// when a new line was appended, (false, nil) when the key was
// already set (commented documentation lines like `# key=...` do
// NOT count as "already set" — they're presumed to be example
// documentation, not an operator decision).
//
// When appending, an optional marker comment can be prepended on its
// own line for traceability (e.g. "# fp init: designer-mode S3").
// Pass an empty markerComment to skip.
//
// Never modifies an existing uncommented assignment — explicit
// operator choices are sacred. This is the primary safety property
// of fp init's env handling.
func EnsureEnvKey(envPath, key, value, markerComment string) (appended bool, err error) {
	if key == "" {
		return false, errors.New("EnsureEnvKey: key is required")
	}
	if strings.ContainsAny(key, "=\n") {
		return false, fmt.Errorf("EnsureEnvKey: key %q must not contain '=' or newlines", key)
	}

	f, err := os.Open(envPath)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", envPath, err)
	}
	scanner := bufio.NewScanner(f)
	// Default token size is 64KB which is plenty for any .env line,
	// but the explicit Buffer call documents the upper bound.
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	prefix := key + "="
	for scanner.Scan() {
		line := strings.TrimLeft(scanner.Text(), " \t")
		// `^# *KEY=` (commented documentation) does NOT count as set.
		// `^KEY=...` (active assignment, possibly with whitespace) does.
		if strings.HasPrefix(line, prefix) {
			f.Close()
			return false, nil
		}
	}
	if err := scanner.Err(); err != nil {
		f.Close()
		return false, fmt.Errorf("scan %s: %w", envPath, err)
	}
	f.Close()

	out, err := os.OpenFile(envPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, fmt.Errorf("open %s for append: %w", envPath, err)
	}
	defer out.Close()

	// Stat to decide whether we need a leading newline. Empty files
	// don't; files that already end in '\n' don't; everything else
	// does (so the appended line doesn't glue onto a trailing line
	// without a terminator).
	info, err := out.Stat()
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", envPath, err)
	}
	needsLeadingNL, err := tailsWithNonNewline(envPath, info.Size())
	if err != nil {
		return false, err
	}
	var b strings.Builder
	if needsLeadingNL {
		b.WriteByte('\n')
	}
	if markerComment != "" {
		b.WriteString(markerComment)
		b.WriteByte('\n')
	}
	b.WriteString(key)
	b.WriteByte('=')
	b.WriteString(value)
	b.WriteByte('\n')
	if _, err := out.WriteString(b.String()); err != nil {
		return false, fmt.Errorf("append to %s: %w", envPath, err)
	}
	return true, nil
}

// tailsWithNonNewline reports whether the file's final byte is
// something other than '\n'. Returns false for empty files (no
// leading newline needed when appending).
func tailsWithNonNewline(path string, size int64) (bool, error) {
	if size == 0 {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open %s for tail check: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Seek(size-1, io.SeekStart); err != nil {
		return false, fmt.Errorf("seek %s: %w", path, err)
	}
	var b [1]byte
	if _, err := f.Read(b[:]); err != nil {
		return false, fmt.Errorf("read tail %s: %w", path, err)
	}
	return b[0] != '\n', nil
}
