// Package prompt holds the interactive input helpers used by
// fp snapshot's slug + note + confirmation prompts.
//
// All helpers take explicit io.Reader / io.Writer args so tests
// can drive them with bytes.Buffer pairs without touching real
// stdin / TTY state.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// IsTerminal reports whether the underlying file is an interactive
// TTY. Used to gate $EDITOR-based note input + the confirmation
// prompt; non-TTY contexts (CI, pipes) take the non-interactive path.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// AskSlug asks the designer for a slug, suggesting `def` as the
// default. Empty input accepts the default. Returns the chosen slug
// (post-cleanup is the caller's responsibility).
func AskSlug(in io.Reader, out io.Writer, def string) (string, error) {
	if def == "" {
		fmt.Fprintf(out, "snapshot slug: ")
	} else {
		fmt.Fprintf(out, "snapshot slug [%s]: ", def)
	}
	line, err := readLine(in)
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// AskNote captures the designer's note. When useEditor is true and
// $EDITOR is set, opens an editor on a temp file (git-commit-style).
// Otherwise reads a single line from stdin.
func AskNote(in io.Reader, out io.Writer, slug string, useEditor bool) (string, error) {
	if useEditor {
		editor := strings.TrimSpace(os.Getenv("EDITOR"))
		if editor != "" {
			return editorNote(slug, editor)
		}
	}
	fmt.Fprintf(out, "snapshot note (single line, empty for none): ")
	line, err := readLine(in)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\n\r"), nil
}

// Confirm prints `question [y/N]: ` and returns true only if the
// designer types y or yes. Anything else (including bare Enter)
// returns false. Matches the plan's "Enter aborts" semantic for the
// uncommitted-changes guard.
func Confirm(in io.Reader, out io.Writer, question string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N]: ", question)
	line, err := readLine(in)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func readLine(in io.Reader) (string, error) {
	r := bufio.NewReader(in)
	s, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return s, nil
}

// editorNote opens $EDITOR on a temporary file pre-populated with a
// "# lines starting with #" header naming the slug, lets the designer
// edit, then strips comments + collapses leading/trailing blank lines.
func editorNote(slug, editor string) (string, error) {
	tmp, err := os.CreateTemp("", "fp-note-*.txt")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }()

	header := fmt.Sprintf(
		"\n# write a note for snapshot %q (multi-line OK)\n# lines starting with # are ignored\n# saving an empty file = empty note\n",
		slug,
	)
	if _, err := tmp.WriteString(header); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %q", editor, path))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q exited: %w", editor, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return stripCommentLines(string(data)), nil
}

func stripCommentLines(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "#") {
			continue
		}
		lines = append(lines, l)
	}
	out := strings.Join(lines, "\n")
	return strings.TrimSpace(out)
}
