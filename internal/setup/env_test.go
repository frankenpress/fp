package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvFileMissing(t *testing.T) {
	dir := t.TempDir()
	missing, err := EnvFileMissing(filepath.Join(dir, "nope"))
	if err != nil {
		t.Fatalf("EnvFileMissing: %v", err)
	}
	if !missing {
		t.Error("missing = false, want true for nonexistent file")
	}

	present := filepath.Join(dir, ".env")
	if err := os.WriteFile(present, []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing, err = EnvFileMissing(present)
	if err != nil {
		t.Fatalf("EnvFileMissing on existing: %v", err)
	}
	if missing {
		t.Error("missing = true, want false for existing file")
	}
}

func TestScaffoldEnvFromExample_CopiesWhenAbsent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("DB_NAME=wp\nDB_USER=wp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldEnvFromExample(root)
	if err != nil {
		t.Fatalf("ScaffoldEnvFromExample: %v", err)
	}
	if !created {
		t.Error("created = false, want true")
	}

	body, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if string(body) != "DB_NAME=wp\nDB_USER=wp\n" {
		t.Errorf(".env content = %q, want copy of .env.example", string(body))
	}
}

func TestScaffoldEnvFromExample_NoOpWhenPresent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("FROM_EXAMPLE=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("ALREADY=here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldEnvFromExample(root)
	if err != nil {
		t.Fatalf("ScaffoldEnvFromExample: %v", err)
	}
	if created {
		t.Error("created = true, want false (existing .env should not be touched)")
	}

	body, _ := os.ReadFile(filepath.Join(root, ".env"))
	if string(body) != "ALREADY=here\n" {
		t.Errorf(".env was modified: %q", string(body))
	}
}

func TestScaffoldEnvFromExample_ErrorsWithoutExample(t *testing.T) {
	root := t.TempDir()
	_, err := ScaffoldEnvFromExample(root)
	if err == nil {
		t.Fatal("expected error when .env.example is missing")
	}
	if !strings.Contains(err.Error(), ".env.example") {
		t.Errorf("error %v missing .env.example mention", err)
	}
}

func TestEnsureEnvKey_AppendsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("DB_NAME=wp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appended, err := EnsureEnvKey(envPath, "FP_S3_DISABLED", "0", "# fp init: designer-mode S3")
	if err != nil {
		t.Fatalf("EnsureEnvKey: %v", err)
	}
	if !appended {
		t.Error("appended = false, want true")
	}

	body, _ := os.ReadFile(envPath)
	want := "DB_NAME=wp\n# fp init: designer-mode S3\nFP_S3_DISABLED=0\n"
	if string(body) != want {
		t.Errorf(".env content = %q, want %q", string(body), want)
	}
}

func TestEnsureEnvKey_NoOpWhenAlreadySetUncommented(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FP_S3_DISABLED=1\nOTHER=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appended, err := EnsureEnvKey(envPath, "FP_S3_DISABLED", "0", "")
	if err != nil {
		t.Fatalf("EnsureEnvKey: %v", err)
	}
	if appended {
		t.Error("appended = true, want false (explicit operator value must be preserved)")
	}

	body, _ := os.ReadFile(envPath)
	if string(body) != "FP_S3_DISABLED=1\nOTHER=x\n" {
		t.Errorf(".env was modified: %q", string(body))
	}
}

func TestEnsureEnvKey_AppendsWhenOnlyCommentedReferenceExists(t *testing.T) {
	// .env.example often documents keys via commented lines. Those
	// are documentation, not an operator decision — should still
	// trigger an append.
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("# FP_S3_DISABLED=1   # documented but not active\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appended, err := EnsureEnvKey(envPath, "FP_S3_DISABLED", "0", "")
	if err != nil {
		t.Fatalf("EnsureEnvKey: %v", err)
	}
	if !appended {
		t.Error("appended = false, want true (commented line shouldn't count as set)")
	}
}

func TestEnsureEnvKey_HandlesIndentedAssignment(t *testing.T) {
	// Some .env files indent keys. The match strips leading
	// whitespace before checking the prefix.
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("  FP_S3_DISABLED=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appended, err := EnsureEnvKey(envPath, "FP_S3_DISABLED", "0", "")
	if err != nil {
		t.Fatalf("EnsureEnvKey: %v", err)
	}
	if appended {
		t.Error("appended = true, want false (indented assignment still counts as set)")
	}
}

func TestEnsureEnvKey_AppendsLeadingNewlineWhenFileLacksTrailing(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	// File without trailing newline.
	if err := os.WriteFile(envPath, []byte("DB_NAME=wp"), 0o644); err != nil {
		t.Fatal(err)
	}

	appended, err := EnsureEnvKey(envPath, "X", "y", "")
	if err != nil {
		t.Fatalf("EnsureEnvKey: %v", err)
	}
	if !appended {
		t.Error("appended = false")
	}

	body, _ := os.ReadFile(envPath)
	// Must not glue X=y onto the previous line.
	if string(body) != "DB_NAME=wp\nX=y\n" {
		t.Errorf(".env content = %q, want DB_NAME=wp\\nX=y\\n", string(body))
	}
}

func TestEnsureEnvKey_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	appended, err := EnsureEnvKey(envPath, "X", "y", "")
	if err != nil {
		t.Fatalf("EnsureEnvKey: %v", err)
	}
	if !appended {
		t.Error("appended = false")
	}

	body, _ := os.ReadFile(envPath)
	if string(body) != "X=y\n" {
		t.Errorf(".env content = %q, want X=y\\n", string(body))
	}
}

func TestEnsureEnvKey_RejectsBadKeys(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	_ = os.WriteFile(envPath, []byte(""), 0o644)

	cases := []string{"", "has=equals", "has\nnewline"}
	for _, k := range cases {
		_, err := EnsureEnvKey(envPath, k, "x", "")
		if err == nil {
			t.Errorf("EnsureEnvKey(%q) returned nil error", k)
		}
	}
}
