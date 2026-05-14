package release

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/gh"
	"github.com/frankenpress/fp/internal/git"
	"github.com/frankenpress/fp/internal/state"
)

// stack assembles the four runners the release pipeline needs.
type stack struct {
	root   string
	cfg    *config.Config
	docker *docker.Fake
	git    *git.Fake
	gh     *gh.Fake
}

func newStack(t *testing.T, slug, currentBranch string) *stack {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	dockerFake := docker.NewFake()
	dockerFake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	dockerFake.CopyFunc = func(_ context.Context, _, dst string) error {
		slugDir := filepath.Join(dst, slug)
		if err := os.MkdirAll(slugDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(slugDir, "manifest.yaml"), []byte(`schema: fp.snapshot/v4
id: x
adapter: fse
source:
  source_theme: twentytwentyfive
author:
  note: ""
contents:
  templates_count: 5
  options_count: 8
  attachments_count: 2
  uploads_file_count: 16
`), 0o644)
	}

	gitFake := git.NewFake()
	gitFake.Branch = currentBranch

	ghFake := gh.NewFake()
	ghFake.PRCreateURL = "https://github.com/frankenpress/fp/pull/999"

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	return &stack{
		root:   root,
		cfg:    cfg,
		docker: dockerFake,
		git:    gitFake,
		gh:     ghFake,
	}
}

func (s *stack) opts(slug, note string) Options {
	return Options{
		Slug:         slug,
		Note:         note,
		Yes:          true, // tests run non-interactive; skip the confirm
		RepoRoot:     s.root,
		Config:       s.cfg,
		State:        &state.State{},
		DockerRunner: s.docker,
		GitRunner:    s.git,
		GHRunner:     s.gh,
		Stdin:        bytes.NewReader(nil),
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Interactive:  false,
	}
}

func TestRun_HappyPath_OnFeatureBranch(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")

	if err := Run(context.Background(), s.opts("sts-launch", "Iterating on footer")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should NOT have checked out a new branch — we were on a feature
	// branch already.
	if s.git.CallCount("Checkout") != 0 {
		t.Errorf("unexpected Checkout on feature branch: %+v", s.git.Calls)
	}
	if s.git.CallCount("Add") != 1 || s.git.CallCount("Commit") != 1 || s.git.CallCount("Push") != 1 {
		t.Errorf("git call counts: %+v", s.git.Calls)
	}
	if s.gh.CallCount("PRCreate") != 1 {
		t.Errorf("expected one gh.PRCreate, got %d", s.gh.CallCount("PRCreate"))
	}

	// Verify commit message subject.
	for _, c := range s.git.Calls {
		if c.Method == "Commit" {
			if !strings.HasPrefix(c.Message, "snapshot: sts-launch") {
				t.Errorf("commit subject = %q, want \"snapshot: sts-launch...\"", c.Message)
			}
			if !strings.Contains(c.Message, "Iterating on footer") {
				t.Errorf("commit body missing note: %q", c.Message)
			}
		}
	}

	// PR title shape.
	for _, c := range s.gh.Calls {
		if c.Method == "PRCreate" {
			if c.Title != "snapshot: sts-launch" {
				t.Errorf("PR title = %q, want \"snapshot: sts-launch\"", c.Title)
			}
			if !strings.Contains(c.Body, "fp.snapshot/v4") {
				t.Errorf("PR body missing schema field:\n%s", c.Body)
			}
			if !strings.Contains(c.Body, "Iterating on footer") {
				t.Errorf("PR body missing designer note:\n%s", c.Body)
			}
		}
	}
}

func TestRun_AutoBranchFromMain(t *testing.T) {
	s := newStack(t, "sts-launch", "main")

	if err := Run(context.Background(), s.opts("sts-launch", "")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have checked out feat/snapshot-sts-launch with create=true.
	foundCheckout := false
	for _, c := range s.git.Calls {
		if c.Method == "Checkout" && c.Branch == "feat/snapshot-sts-launch" {
			foundCheckout = true
			if !c.Create {
				t.Errorf("expected create=true for new feature branch, got %+v", c)
			}
		}
	}
	if !foundCheckout {
		t.Errorf("did not check out feat/snapshot-sts-launch: %+v", s.git.Calls)
	}
}

func TestRun_AutoBranchFromMain_ExistingBranchReused(t *testing.T) {
	s := newStack(t, "sts-launch", "main")
	s.git.ExistingBranches["feat/snapshot-sts-launch"] = true

	if err := Run(context.Background(), s.opts("sts-launch", "")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, c := range s.git.Calls {
		if c.Method == "Checkout" && c.Branch == "feat/snapshot-sts-launch" {
			if c.Create {
				t.Errorf("expected create=false for existing branch, got %+v", c)
			}
		}
	}
}

func TestRun_ExplicitBranchOverridesPolicy(t *testing.T) {
	s := newStack(t, "sts-launch", "main")

	opts := s.opts("sts-launch", "")
	opts.Branch = "custom-release"

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	foundCheckout := false
	for _, c := range s.git.Calls {
		if c.Method == "Checkout" && c.Branch == "custom-release" {
			foundCheckout = true
		}
	}
	if !foundCheckout {
		t.Errorf("did not check out custom-release: %+v", s.git.Calls)
	}
}

func TestRun_Draft_PassesDraftToGH(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")

	var stdout bytes.Buffer
	opts := s.opts("sts-launch", "")
	opts.Draft = true
	opts.Stdout = &stdout

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	foundDraft := false
	for _, c := range s.gh.Calls {
		if c.Method == "PRCreate" {
			if !c.Draft {
				t.Errorf("expected PRCreate with Draft=true, got Draft=false")
			}
			foundDraft = true
		}
	}
	if !foundDraft {
		t.Errorf("did not see a PRCreate call: %+v", s.gh.Calls)
	}
	if !strings.Contains(stdout.String(), "opened draft PR") {
		t.Errorf("stdout missing draft PR confirmation:\n%s", stdout.String())
	}
}

func TestRun_NoDraft_DefaultsToNonDraft(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")

	if err := Run(context.Background(), s.opts("sts-launch", "")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, c := range s.gh.Calls {
		if c.Method == "PRCreate" && c.Draft {
			t.Errorf("expected non-draft PRCreate, got Draft=true")
		}
	}
}

func TestRun_NoPR_SkipsGHCreate(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")

	opts := s.opts("sts-launch", "")
	opts.NoPR = true

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if s.gh.CallCount("PRCreate") != 0 {
		t.Errorf("--no-pr should skip PRCreate; got %d calls", s.gh.CallCount("PRCreate"))
	}
}

func TestRun_PushFailure_PrintsRecoveryHint(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")
	s.git.PushErr = errors.New("permission denied")

	var stderr bytes.Buffer
	opts := s.opts("sts-launch", "")
	opts.Stderr = &stderr

	err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected push failure")
	}
	if !strings.Contains(err.Error(), "git push") {
		t.Errorf("error missing git push context: %v", err)
	}
	if !strings.Contains(err.Error(), "retry the push manually") {
		t.Errorf("error missing recovery hint: %v", err)
	}
}

func TestRun_PRAlreadyExists_SurfacesExistingURL(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")
	s.gh.PRCreateErr = errors.New("a pull request already exists")
	s.gh.PRViewURL = "https://github.com/frankenpress/fp/pull/42"

	var stdout bytes.Buffer
	opts := s.opts("sts-launch", "")
	opts.Stdout = &stdout

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout.String(), "existing PR for feat/footer: https://github.com/frankenpress/fp/pull/42") {
		t.Errorf("stdout missing existing-PR line:\n%s", stdout.String())
	}
}

func TestRun_PRCreateFailure_NoExistingPR_SurfacesError(t *testing.T) {
	s := newStack(t, "sts-launch", "feat/footer")
	s.gh.PRCreateErr = errors.New("api timeout")
	// PRView returns empty (no existing PR).

	err := Run(context.Background(), s.opts("sts-launch", ""))
	if err == nil {
		t.Fatal("expected PR-create failure to surface as error")
	}
	if !strings.Contains(err.Error(), "gh pr create") {
		t.Errorf("error missing gh pr create context: %v", err)
	}
}

func TestBuildPRBody(t *testing.T) {
	body := buildPRBody("sts-launch", "Iterating", nil)
	for _, want := range []string{
		"## Snapshot",
		"sts-launch",
		"## Designer note",
		"Iterating",
		"## Apply path",
		"/app/web/imports/sts-launch",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("PR body missing %q:\n%s", want, body)
		}
	}
}

func TestBuildPRBody_EmptyNote(t *testing.T) {
	body := buildPRBody("sts-launch", "", nil)
	if !strings.Contains(body, "(none)") {
		t.Errorf("PR body for empty note should say (none):\n%s", body)
	}
}

func TestBuildCommitMessage(t *testing.T) {
	if got := buildCommitMessage("sts-launch", ""); got != "snapshot: sts-launch" {
		t.Errorf("buildCommitMessage(no note) = %q", got)
	}
	got := buildCommitMessage("sts-launch", "Iterating on footer")
	want := "snapshot: sts-launch\n\nIterating on footer"
	if got != want {
		t.Errorf("buildCommitMessage = %q, want %q", got, want)
	}
}
