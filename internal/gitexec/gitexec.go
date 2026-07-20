package gitexec

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Exit codes floppygit returns. A user-fixable fault (dirty tree, conflict, bad
// template) is distinct from an environment fault (git or gh missing) so a
// caller - a shell, a CI step, an agent - can tell whether a rerun after fixing
// the working copy could succeed, or whether the machine itself needs setup.
const (
	ExitOK   = 0
	ExitUser = 3
	ExitEnv  = 4
)

// faultError carries the exit code floppygit should return for a classified
// failure. It is created only through UserFault and EnvFault.
type faultError struct {
	code int
	msg  string
}

func (e *faultError) Error() string { return e.msg }

// UserFault marks a failure the user can fix and rerun: a conflict, a dirty
// tree, a broken commit template.
func UserFault(msg string) error { return &faultError{code: ExitUser, msg: msg} }

// EnvFault marks a failure of the environment: git or gh missing, no remote.
func EnvFault(msg string) error { return &faultError{code: ExitEnv, msg: msg} }

// CodeOf returns the exit code for an error: 0 for nil, the fault's code for a
// classified fault (seen through %w wrapping), and 1 for any other failure.
func CodeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	var fe *faultError
	if errors.As(err, &fe) {
		return fe.code
	}
	return 1
}

// ExitCode extracts the exit status of a failed command, seeing through %w
// wrapping. It returns 0 for a nil error and 1 for a non-exit failure.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// Git runs git in a working directory. Every floppygit git operation goes
// through here: it is the real git binary, so commit-msg and pre-push hooks,
// credential helpers, and local config all fire exactly as under plain git.
type Git struct {
	// Dir is the working directory git runs in; empty means the process cwd.
	Dir string
	// Env is appended to the process environment for each command, used to set
	// GIT_SEQUENCE_EDITOR/GIT_EDITOR for a non-interactive autosquash rebase.
	Env []string
}

func (g Git) command(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.Dir
	if len(g.Env) > 0 {
		cmd.Env = append(os.Environ(), g.Env...)
	}
	return cmd
}

// Output runs git and returns its trimmed stdout. On failure the error carries
// the command, git's error, and git's stderr, and still unwraps to the
// *exec.ExitError so ExitCode can read git's status.
func (g Git) Output(args ...string) (string, error) {
	cmd := g.command(args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Run runs git with the terminal's stdio inherited, so hooks, editors, progress,
// and credential prompts reach the user unchanged. It returns git's error.
func (g Git) Run(args ...string) error {
	cmd := g.command(args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Passthrough runs git verbatim with inherited stdio and returns git's own exit
// code, so any subcommand floppygit does not own behaves exactly like plain git.
func Passthrough(g Git, args []string) int {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.Dir
	if len(g.Env) > 0 {
		cmd.Env = append(os.Environ(), g.Env...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return ExitCode(cmd.Run())
}

// GH is the slice of the GitHub CLI floppygit needs: open and re-sync one-commit
// pull requests, arm auto-merge, and fall back to watching checks. Tests fake it
// so PR and auto-merge behavior is exercised with no network.
type GH interface {
	// ViewPR reports whether an open PR already exists for the head branch.
	ViewPR(branch string) (bool, error)
	// CreatePR opens a PR for branch against base with the given title and body.
	CreatePR(branch, base, title, body string) error
	// EditPR re-syncs an existing PR's base, title, and body after an amend.
	EditPR(branch, base, title, body string) error
	// AutoMerge arms auto-merge (squash, delete branch) for branch.
	AutoMerge(branch string) error
	// WatchChecks blocks until branch's checks finish; the auto-merge fallback.
	WatchChecks(branch string) error
	// Merge merges branch now (squash, delete branch); used after checks pass.
	Merge(branch string) error
}

// RealGH implements GH by shelling out to the `gh` CLI, so PRs, auto-merge, and
// check watching use the user's existing GitHub authentication.
type RealGH struct {
	// Dir is the working directory gh runs in; empty means the process cwd.
	Dir string
}

// NewGH returns a GH backed by the gh CLI running in dir.
func NewGH(dir string) GH { return RealGH{Dir: dir} }

// ViewPR probes for an open PR; any failure is read as "no PR yet", matching
// scripts/pr.sh, which treats a non-zero `gh pr view` as no PR.
func (h RealGH) ViewPR(branch string) (bool, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number")
	cmd.Dir = h.Dir
	return cmd.Run() == nil, nil
}

func (h RealGH) CreatePR(branch, base, title, body string) error {
	return h.stream("pr", "create", "--head", branch, "--base", base, "--title", title, "--body", body)
}

func (h RealGH) EditPR(branch, base, title, body string) error {
	return h.stream("pr", "edit", branch, "--base", base, "--title", title, "--body", body)
}

func (h RealGH) AutoMerge(branch string) error {
	return h.stream("pr", "merge", branch, "--auto", "--squash", "--delete-branch")
}

func (h RealGH) WatchChecks(branch string) error {
	return h.stream("pr", "checks", branch, "--watch")
}

func (h RealGH) Merge(branch string) error {
	return h.stream("pr", "merge", branch, "--squash", "--delete-branch")
}

func (h RealGH) stream(args ...string) error {
	cmd := exec.Command("gh", args...)
	cmd.Dir = h.Dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
