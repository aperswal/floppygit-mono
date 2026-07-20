// Package gittest is test-support scaffolding for floppygit's suite. It drives a
// real system git binary against throwaway repositories created under a test's
// temp dir, and it provides a fake gh runner (see gh.go) so PR and auto-merge
// behavior can be exercised with no network and no real GitHub.
//
// It deliberately does not import any of floppygit's own packages: the code
// under test constructs its git handle from Repo.Dir itself, so this package
// compiles and its real-git helpers stay usable even before the implementation
// exists. That keeps `go build ./...` green while the *_test.go files are the
// only things that go red on missing implementation.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a throwaway git repository backed by a temp directory and driven by
// the real system git binary.
type Repo struct {
	// Dir is the working tree root; the code under test builds its git handle
	// from this path (for example gitexec.Git{Dir: repo.Dir}).
	Dir string
	t   testing.TB
}

// git runs a git command in the repo, isolating it from the developer's global
// configuration so hooks fire exactly as installed and commits never trip over a
// machine-wide gpg-sign or hooks-path setting.
func gitRaw(t testing.TB, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=floppy test",
		"GIT_AUTHOR_EMAIL=test@floppygit.example",
		"GIT_COMMITTER_NAME=floppy test",
		"GIT_COMMITTER_EMAIL=test@floppygit.example",
	)
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

// configureLocal pins the identity, disables commit signing, and forces the
// hooks path to the repo's own .git/hooks so neither the developer's global
// config nor the code under test's ambient environment changes the outcome.
func configureLocal(t testing.TB, dir string) {
	t.Helper()
	hooks := filepath.Join(dir, ".git", "hooks")
	pairs := [][2]string{
		{"user.name", "floppy test"},
		{"user.email", "test@floppygit.example"},
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
		{"core.hooksPath", hooks},
		{"init.defaultBranch", "main"},
		{"advice.detachedHead", "false"},
	}
	for _, p := range pairs {
		if out, err := gitRaw(t, dir, "config", "--local", p[0], p[1]); err != nil {
			t.Fatalf("git config %s: %v\n%s", p[0], err, out)
		}
	}
}

// NewRepo initializes an empty repo on branch main with a single seed commit so
// HEAD, branches, and diffs all have somewhere to stand.
func NewRepo(t testing.TB) *Repo {
	t.Helper()
	dir := t.TempDir()
	if out, err := gitRaw(t, dir, "init", "-b", "main", dir); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	configureLocal(t, dir)
	r := &Repo{Dir: dir, t: t}
	r.WriteFile("README.md", "seed\n")
	r.Add("README.md")
	r.Commit("ops(seed): initial commit for the throwaway test repository here")
	return r
}

// Git runs git in the repo and fails the test on error, returning trimmed stdout.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	out, err := gitRaw(r.t, r.Dir, args...)
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// TryGit runs git and returns its combined output and error without failing the
// test, for the cases that expect git itself to refuse.
func (r *Repo) TryGit(args ...string) (string, error) {
	r.t.Helper()
	return gitRaw(r.t, r.Dir, args...)
}

// WriteFile writes content to a repo-relative path, creating parent dirs.
func (r *Repo) WriteFile(rel, content string) {
	r.t.Helper()
	full := filepath.Join(r.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", rel, err)
	}
}

// Add stages the given repo-relative paths.
func (r *Repo) Add(paths ...string) {
	r.t.Helper()
	r.Git(append([]string{"add"}, paths...)...)
}

// Commit stages everything and records a commit with the given message.
func (r *Repo) Commit(msg string) {
	r.t.Helper()
	r.Git("add", "-A")
	r.Git("commit", "-m", msg)
}

// CommitFile writes, stages, and commits a single file in one step.
func (r *Repo) CommitFile(rel, content, msg string) {
	r.t.Helper()
	r.WriteFile(rel, content)
	r.Add(rel)
	r.Git("commit", "-m", msg)
}

// Head returns the full SHA of HEAD.
func (r *Repo) Head() string { return r.RevParse("HEAD") }

// RevParse resolves a ref to its full SHA.
func (r *Repo) RevParse(ref string) string {
	r.t.Helper()
	return r.Git("rev-parse", ref)
}

// Branch returns the current branch name, or "" when HEAD is detached.
func (r *Repo) Branch() string {
	r.t.Helper()
	out, _ := r.TryGit("symbolic-ref", "--short", "-q", "HEAD")
	return strings.TrimSpace(out)
}

// BranchExists reports whether a local branch ref is present.
func (r *Repo) BranchExists(name string) bool {
	r.t.Helper()
	_, err := r.TryGit("rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// ConfigValue returns a git config value, or "" when unset.
func (r *Repo) ConfigValue(key string) string {
	r.t.Helper()
	out, _ := r.TryGit("config", "--local", "--get", key)
	return strings.TrimSpace(out)
}

// FloppyParent returns branch.<name>.floppyParent, or "" when unset.
func (r *Repo) FloppyParent(name string) string {
	r.t.Helper()
	return r.ConfigValue("branch." + name + ".floppyParent")
}

// SetFloppyParent records a stack parent, matching what `floppygit new` writes.
func (r *Repo) SetFloppyParent(name, parent string) {
	r.t.Helper()
	r.Git("config", "--local", "branch."+name+".floppyParent", parent)
}

// FileAt returns the content of a repo-relative path at the given ref.
func (r *Repo) FileAt(ref, rel string) string {
	r.t.Helper()
	return r.Git("show", ref+":"+rel)
}

// Subjects returns the commit subjects in the range, newest last.
func (r *Repo) Subjects(revRange string) []string {
	r.t.Helper()
	out := r.Git("log", "--reverse", "--format=%s", revRange)
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// CountRange returns how many commits are in the given rev range.
func (r *Repo) CountRange(revRange string) int {
	r.t.Helper()
	out := r.Git("rev-list", "--count", revRange)
	n := 0
	for _, c := range out {
		if c < '0' || c > '9' {
			r.t.Fatalf("unexpected rev-list --count output %q", out)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// IsAncestor reports whether ancestor is an ancestor of descendant.
func (r *Repo) IsAncestor(ancestor, descendant string) bool {
	r.t.Helper()
	_, err := r.TryGit("merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// Dirty reports whether the working tree or index has uncommitted changes.
func (r *Repo) Dirty() bool {
	r.t.Helper()
	out, _ := r.TryGit("status", "--porcelain")
	return strings.TrimSpace(out) != ""
}

// RebaseInProgress reports whether an interrupted rebase is sitting in .git,
// the "half-restacked state" floppygit must never leave behind.
func (r *Repo) RebaseInProgress() bool {
	r.t.Helper()
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(r.Dir, ".git", d)); err == nil {
			return true
		}
	}
	return false
}

// InstallHook writes an executable git hook (for example commit-msg) into the
// repo's hooks path. The script is run verbatim by git, so a hook that exits
// non-zero rejects the operation, proving floppygit shelled out to real git.
func (r *Repo) InstallHook(name, script string) {
	r.t.Helper()
	hooks := filepath.Join(r.Dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		r.t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0o755); err != nil {
		r.t.Fatalf("write hook %s: %v", name, err)
	}
}

// Detach moves HEAD off any branch, for the "refuse on detached HEAD" cases.
func (r *Repo) Detach() {
	r.t.Helper()
	r.Git("checkout", "--detach", "HEAD")
}

// NewOrigin creates a bare repository seeded with one commit on main and returns
// its path, suitable as a push/fetch remote for a clone.
func NewOrigin(t testing.TB) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	if out, err := gitRaw(t, t.TempDir(), "init", "--bare", "-b", "main", origin); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	// Seed main through a throwaway clone; a bare repo cannot commit directly.
	seed := t.TempDir()
	if out, err := gitRaw(t, t.TempDir(), "clone", origin, seed); err != nil {
		t.Fatalf("git clone seed: %v\n%s", err, out)
	}
	configureLocal(t, seed)
	sr := &Repo{Dir: seed, t: t}
	sr.WriteFile("base.txt", "base\n")
	sr.WriteFile("README.md", "origin seed\n")
	sr.Add("base.txt", "README.md")
	sr.Git("commit", "-m", "ops(seed): seed origin main with a base file for tests here")
	sr.Git("push", "-u", "origin", "main")
	return origin
}

// Clone checks out origin into a fresh temp dir tracking origin/main.
func Clone(t testing.TB, origin string) *Repo {
	t.Helper()
	dir := t.TempDir()
	if out, err := gitRaw(t, t.TempDir(), "clone", origin, dir); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	configureLocal(t, dir)
	return &Repo{Dir: dir, t: t}
}

// AdvanceOriginMain lands a new commit on origin's main without touching the
// caller's clone, simulating another PR merging while a stack is in flight. When
// rel names a file that also exists in the caller's feature work, editing it
// here is how a conflict against origin/main is staged.
func AdvanceOriginMain(t testing.TB, origin, rel, content, msg string) {
	t.Helper()
	tmp := Clone(t, origin)
	tmp.WriteFile(rel, content)
	tmp.Add(rel)
	tmp.Git("commit", "-m", msg)
	tmp.Git("push", "origin", "main")
}

// SquashMergeToOrigin simulates a squash merge of a one-commit branch: it lands
// a commit on origin/main whose patch is identical to the branch's single
// commit, so a later rebase drops that commit as already-upstream. The caller's
// clone is untouched.
func SquashMergeToOrigin(t testing.TB, origin, branchSHA, msg string) {
	t.Helper()
	tmp := Clone(t, origin)
	tmp.Git("fetch", "origin", branchSHA)
	// A one-commit branch's cherry-pick onto main is patch-identical to a squash
	// merge of it; that patch id is what makes the later rebase skip it.
	tmp.Git("cherry-pick", branchSHA)
	tmp.Git("push", "origin", "main")
}

// DeleteOriginBranch removes a branch ref from the bare origin, simulating
// GitHub deleting a head branch when its PR merges. After a `fetch --prune` the
// local branch's upstream then reads as [gone], the signal floppygit prunes on.
func DeleteOriginBranch(t testing.TB, origin, name string) {
	t.Helper()
	if out, err := gitRaw(t, origin, "update-ref", "-d", "refs/heads/"+name); err != nil {
		t.Fatalf("delete origin branch %s: %v\n%s", name, err, out)
	}
}

// RemoteSHA returns the SHA a ref points to inside the bare origin.
func RemoteSHA(t testing.TB, origin, ref string) (string, bool) {
	t.Helper()
	out, err := gitRaw(t, origin, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}
