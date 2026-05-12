package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

// Render writes a human-readable summary of res to w. Format is
// terminal-friendly text, sections elided when empty. The leading
// header carries the two snapshot paths (basename-only by default
// for tidiness — full paths land in the verbose section).
func Render(w io.Writer, res *Result) {
	a := filepath.Base(res.A.Path)
	b := filepath.Base(res.B.Path)

	fmt.Fprintf(w, "diff %s -> %s\n", a, b)
	fmt.Fprintf(w, "  a: %s\n", res.A.Path)
	fmt.Fprintf(w, "  b: %s\n\n", res.B.Path)

	if res.Empty() {
		fmt.Fprintln(w, "no structural differences.")
		return
	}

	if len(res.ManifestChanges) > 0 {
		fmt.Fprintln(w, "manifest:")
		for _, c := range res.ManifestChanges {
			fmt.Fprintf(w, "  ~ %s: %s -> %s\n", c.Field, quoteIfEmpty(c.A), quoteIfEmpty(c.B))
		}
		fmt.Fprintln(w)
	}

	if hasTemplateChanges(res) {
		fmt.Fprintf(w, "templates (%d -> %d):\n",
			countOr(res.A, func(s *Snapshot) int { return len(s.Templates) }),
			countOr(res.B, func(s *Snapshot) int { return len(s.Templates) }),
		)
		for _, k := range res.TemplatesAdded {
			fmt.Fprintf(w, "  + %s\n", k)
		}
		for _, k := range res.TemplatesRemoved {
			fmt.Fprintf(w, "  - %s\n", k)
		}
		for _, c := range res.TemplatesModified {
			if c.BothTitle {
				fmt.Fprintf(w, "  ~ %s (content modified)\n", c.Key)
			} else {
				fmt.Fprintf(w, "  ~ %s (title: %q -> %q, content modified)\n", c.Key, c.TitleA, c.TitleB)
			}
		}
		fmt.Fprintln(w)
	}

	if hasOptionsChanges(res) {
		fmt.Fprintf(w, "options (%d -> %d):\n",
			countOr(res.A, func(s *Snapshot) int { return len(s.Options) }),
			countOr(res.B, func(s *Snapshot) int { return len(s.Options) }),
		)
		for _, k := range res.OptionsAdded {
			fmt.Fprintf(w, "  + %s = %s\n", k, displayValue(res.B.Options[k]))
		}
		for _, k := range res.OptionsRemoved {
			fmt.Fprintf(w, "  - %s = %s\n", k, displayValue(res.A.Options[k]))
		}
		for _, c := range res.OptionsChanged {
			fmt.Fprintf(w, "  ~ %s: %s -> %s\n", c.Key, displayValue(c.A), displayValue(c.B))
		}
		fmt.Fprintln(w)
	}

	if len(res.ThemeModsChanged) > 0 {
		fmt.Fprintln(w, "theme_mods:")
		for _, tm := range res.ThemeModsChanged {
			fmt.Fprintf(w, "  [%s]\n", tm.Stylesheet)
			for _, k := range tm.Added {
				fmt.Fprintf(w, "    + %s = %s\n", k, displayValue(res.B.ThemeMods[tm.Stylesheet][k]))
			}
			for _, k := range tm.Removed {
				fmt.Fprintf(w, "    - %s = %s\n", k, displayValue(res.A.ThemeMods[tm.Stylesheet][k]))
			}
			for _, c := range tm.Changed {
				fmt.Fprintf(w, "    ~ %s: %s -> %s\n", c.Key, displayValue(c.A), displayValue(c.B))
			}
		}
		fmt.Fprintln(w)
	}

	if hasAttachmentChanges(res) {
		fmt.Fprintf(w, "attachments (%d -> %d):\n",
			countOr(res.A, func(s *Snapshot) int { return len(s.Attachments) }),
			countOr(res.B, func(s *Snapshot) int { return len(s.Attachments) }),
		)
		for _, k := range res.AttachmentsAdded {
			fmt.Fprintf(w, "  + %s\n", k)
		}
		for _, k := range res.AttachmentsRemoved {
			fmt.Fprintf(w, "  - %s\n", k)
		}
		fmt.Fprintln(w)
	}

	if hasUploadsChanges(res) {
		fmt.Fprintf(w, "uploads (%d files -> %d files):\n",
			countOr(res.A, func(s *Snapshot) int { return len(s.Uploads) }),
			countOr(res.B, func(s *Snapshot) int { return len(s.Uploads) }),
		)
		for _, k := range res.UploadsAdded {
			fmt.Fprintf(w, "  + %s\n", k)
		}
		for _, k := range res.UploadsRemoved {
			fmt.Fprintf(w, "  - %s\n", k)
		}
		for _, c := range res.UploadsChanged {
			fmt.Fprintf(w, "  ~ %s (sha %s.. -> %s..)\n", c.Path, short(c.ShaA), short(c.ShaB))
		}
	}
}

// displayValue renders any JSON value compactly, with long strings
// truncated to ~60 chars so terminal output stays readable.
func displayValue(v any) string {
	const maxLen = 60
	switch t := v.(type) {
	case string:
		if len(t) > maxLen {
			return fmt.Sprintf("%q (truncated, %d chars)", t[:maxLen-3]+"...", len(t))
		}
		return fmt.Sprintf("%q", t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		if len(b) > maxLen {
			return string(b[:maxLen-3]) + "..."
		}
		return string(b)
	}
}

func quoteIfEmpty(s string) string {
	if s == "" {
		return `""`
	}
	return s
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func countOr(s *Snapshot, f func(*Snapshot) int) int {
	if s == nil {
		return 0
	}
	return f(s)
}

func hasTemplateChanges(r *Result) bool {
	return len(r.TemplatesAdded)+len(r.TemplatesRemoved)+len(r.TemplatesModified) > 0
}

func hasOptionsChanges(r *Result) bool {
	return len(r.OptionsAdded)+len(r.OptionsRemoved)+len(r.OptionsChanged) > 0
}

func hasAttachmentChanges(r *Result) bool {
	return len(r.AttachmentsAdded)+len(r.AttachmentsRemoved) > 0
}

func hasUploadsChanges(r *Result) bool {
	return len(r.UploadsAdded)+len(r.UploadsRemoved)+len(r.UploadsChanged) > 0
}
