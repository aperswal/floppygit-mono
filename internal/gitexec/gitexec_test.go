package gitexec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
	"github.com/aperswal/floppygit-mono/internal/gittest"
)

func TestOutputCapturesTrimmedStdout(t *testing.T) {
	r := gittest.NewRepo(t)
	g := gitexec.Git{Dir: r.Dir}

	branch, err := g.Output("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if branch != "main" {
		t.Errorf("current branch = %q, want main", branch)
	}

	email, err := g.Output("config", "--get", "user.email")
	if err != nil {
		t.Fatalf("Output config: %v", err)
	}
	if email != "test@floppygit.example" {
		t.Errorf("user.email = %q, want the repo-local identity", email)
	}
}

func TestOutputReturnsErrorOnGitFailure(t *testing.T) {
	r := gittest.NewRepo(t)
	g := gitexec.Git{Dir: r.Dir}

	out, err := g.Output("rev-parse", "--verify", "no-such-ref")
	if err == nil {
		t.Fatalf("Output on a bad ref should error, got %q", out)
	}
}

func TestRunReturnsErrorWithExitCode(t *testing.T) {
	r := gittest.NewRepo(t)
	g := gitexec.Git{Dir: r.Dir}

	if err := g.Run("status", "--porcelain"); err != nil {
		t.Fatalf("Run status should succeed: %v", err)
	}

	err := g.Run("checkout", "definitely-not-a-branch")
	if err == nil {
		t.Fatalf("Run on a missing branch should error")
	}
	if code := gitexec.ExitCode(err); code == 0 {
		t.Errorf("ExitCode = 0 on a failed git command, want the non-zero git code")
	}
}

// Passthrough runs git verbatim and returns git's own exit code, so any
// unrecognized floppygit subcommand behaves exactly like plain git.
func TestPassthroughRunsGitVerbatim(t *testing.T) {
	r := gittest.NewRepo(t)
	g := gitexec.Git{Dir: r.Dir}

	if code := gitexec.Passthrough(g, []string{"rev-parse", "HEAD"}); code != 0 {
		t.Errorf("passthrough of a valid command returned %d, want 0", code)
	}

	// Arguments reach real git and take effect: a checkout actually happens.
	if code := gitexec.Passthrough(g, []string{"checkout", "-b", "sidebar"}); code != 0 {
		t.Fatalf("passthrough checkout returned %d, want 0", code)
	}
	if got := r.Branch(); got != "sidebar" {
		t.Errorf("after passthrough checkout, branch = %q, want sidebar", got)
	}

	if code := gitexec.Passthrough(g, []string{"rev-parse", "--verify", "no-such-ref"}); code == 0 {
		t.Errorf("passthrough of a failing command returned 0, want git's non-zero code")
	}
}

// Every git operation shells out, so repo hooks fire through the wrapper: a
// commit-msg hook that exits non-zero blocks the commit at the gitexec layer.
func TestRunFiresRepoHooks(t *testing.T) {
	r := gittest.NewRepo(t)
	r.InstallHook("commit-msg", "#!/bin/sh\nexit 1\n")
	g := gitexec.Git{Dir: r.Dir}
	before := r.Head()

	err := g.Run("commit", "--allow-empty", "-m", "should be rejected by the hook")
	if err == nil {
		t.Fatalf("commit should fail when the commit-msg hook rejects it")
	}
	if r.Head() != before {
		t.Errorf("HEAD moved despite a rejecting hook: %s -> %s", before, r.Head())
	}
}

// Exit codes separate a user-fixable fault (conflict, dirty tree, template) from
// an environment fault (git or gh missing), so main can exit distinguishably.
func TestFaultClassification(t *testing.T) {
	if gitexec.ExitUser == gitexec.ExitEnv {
		t.Fatalf("ExitUser and ExitEnv must differ")
	}
	if gitexec.CodeOf(nil) != 0 {
		t.Errorf("CodeOf(nil) = %d, want 0", gitexec.CodeOf(nil))
	}

	user := gitexec.UserFault("working tree is dirty; commit or stash first")
	if gitexec.CodeOf(user) != gitexec.ExitUser {
		t.Errorf("CodeOf(UserFault) = %d, want ExitUser (%d)", gitexec.CodeOf(user), gitexec.ExitUser)
	}
	if !strings.Contains(user.Error(), "dirty") {
		t.Errorf("UserFault should carry its message, got %q", user.Error())
	}

	env := gitexec.EnvFault("gh not found on PATH; install the GitHub CLI")
	if gitexec.CodeOf(env) != gitexec.ExitEnv {
		t.Errorf("CodeOf(EnvFault) = %d, want ExitEnv (%d)", gitexec.CodeOf(env), gitexec.ExitEnv)
	}

	// Classification survives wrapping.
	wrapped := fmt.Errorf("push failed: %w", user)
	if gitexec.CodeOf(wrapped) != gitexec.ExitUser {
		t.Errorf("CodeOf(wrapped UserFault) = %d, want ExitUser", gitexec.CodeOf(wrapped))
	}
}
