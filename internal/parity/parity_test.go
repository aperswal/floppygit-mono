package parity_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
	"github.com/aperswal/floppygit-mono/internal/gittest"
	"github.com/aperswal/floppygit-mono/internal/parity"
)

func deps(r *gittest.Repo, fake *gittest.FakeGH, out *bytes.Buffer) parity.Deps {
	return parity.Deps{Git: gitexec.Git{Dir: r.Dir}, GH: fake, Out: out}
}

// pr.sh refuses to run on main; a feature branch is required.
func TestRefusesOnMain(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin) // on main
	fake := gittest.NewFakeGH()

	err := parity.Run(deps(r, fake, &bytes.Buffer{}))
	if err == nil {
		t.Fatalf("Run on main should be refused")
	}
	if !strings.Contains(err.Error(), "feature branch") {
		t.Errorf("refusal should tell the user to switch to a feature branch, got: %q", err.Error())
	}
	if fake.CountOp("create") != 0 {
		t.Errorf("no PR should be opened when refusing on main")
	}
}

// pr.sh also refuses on a detached HEAD (no branch to name the PR).
func TestRefusesOnDetachedHead(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Detach()
	fake := gittest.NewFakeGH()

	if err := parity.Run(deps(r, fake, &bytes.Buffer{})); err == nil {
		t.Fatalf("Run on a detached HEAD should be refused")
	}
}

// fetch --prune, then delete local branches whose upstream is [gone]. A branch
// with no upstream (unpushed local work) and the current branch are never
// touched.
func TestPrunesGoneBranchesOnly(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)

	// zombie: pushed, then its remote branch is deleted (as a merge would).
	r.Git("checkout", "-b", "zombie")
	r.CommitFile("z.txt", "z\n", "feat(z): zombie work whose remote branch will be deleted upstream")
	r.Git("push", "-u", "origin", "zombie")
	gittest.DeleteOriginBranch(t, origin, "zombie")

	// keeper: local-only, no upstream, must survive.
	r.Git("checkout", "main")
	r.Git("branch", "keeper")

	// feat: the branch pr runs on.
	r.Git("checkout", "-b", "feat")
	r.CommitFile("f.txt", "f\n", "feat(f): the single feature commit for this pull request flow")

	var out bytes.Buffer
	fake := gittest.NewFakeGH()
	if err := parity.Run(deps(r, fake, &out)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if r.BranchExists("zombie") {
		t.Errorf("zombie's upstream is gone; it should have been pruned")
	}
	if !r.BranchExists("keeper") {
		t.Errorf("keeper has no upstream and must never be pruned")
	}
	if !r.BranchExists("feat") {
		t.Errorf("the current branch must never be pruned")
	}
	if !strings.Contains(out.String(), "zombie") {
		t.Errorf("pruning should be reported, got: %q", out.String())
	}
}

// A branch that conflicts with the latest origin/main is a hard stop with the
// rebase remedy printed, not an unmergeable PR.
func TestConflictWithMainStopsWithRemedy(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	r.WriteFile("base.txt", "feature-change\n")
	r.Add("base.txt")
	r.Git("commit", "-m", "feat(f): change base to a feature value that conflicts with main")
	gittest.AdvanceOriginMain(t, origin, "base.txt", "origin-change\n",
		"ops(main): change base on origin so the feature branch conflicts now")
	headBefore := r.Head()

	fake := gittest.NewFakeGH()
	err := parity.Run(deps(r, fake, &bytes.Buffer{}))
	if err == nil {
		t.Fatalf("a branch conflicting with origin/main should stop")
	}
	if !strings.Contains(err.Error(), "git rebase origin/main") {
		t.Errorf("conflict stop should print the rebase remedy, got: %q", err.Error())
	}
	if fake.CountOp("create") != 0 {
		t.Errorf("no PR should be opened on a conflict stop")
	}
	if r.Head() != headBefore {
		t.Errorf("a conflict stop must not move the branch")
	}
}

// Behind origin/main, conflict-free, clean tree: rebase automatically, then push
// and open the PR.
func TestCleanTreeRebasesOntoMain(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	r.CommitFile("feat.txt", "feat\n", "feat(f): add a feature file that does not conflict with main at all")
	gittest.AdvanceOriginMain(t, origin, "newmain.txt", "m\n",
		"ops(main): add an unrelated file on origin main with no conflict here")

	var out bytes.Buffer
	fake := gittest.NewFakeGH()
	if err := parity.Run(deps(r, fake, &out)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.IsAncestor("origin/main", "HEAD") {
		t.Errorf("a clean branch behind main should have been rebased onto origin/main")
	}
	if !strings.Contains(out.String(), "rebased") {
		t.Errorf("the automatic rebase should be reported, got: %q", out.String())
	}
	if fake.CountOp("create") != 1 {
		t.Errorf("after rebasing, the PR should be opened")
	}
}

// Behind origin/main, conflict-free, but a dirty tree: warn and continue without
// rebasing (the squash merge stays clean regardless); the PR still opens.
func TestDirtyTreeWarnsAndContinuesWithoutRebase(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	r.CommitFile("feat.txt", "feat\n", "feat(f): add a feature file for the dirty tree continue path here")
	gittest.AdvanceOriginMain(t, origin, "newmain.txt", "m\n",
		"ops(main): add an unrelated file on origin so feat is behind but clean")
	// Dirty the tree with an uncommitted change to a tracked file.
	r.WriteFile("base.txt", "locally-modified\n")

	var out bytes.Buffer
	fake := gittest.NewFakeGH()
	if err := parity.Run(deps(r, fake, &out)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.IsAncestor("origin/main", "HEAD") {
		t.Errorf("a dirty tree must skip the local rebase; HEAD should still be behind origin/main")
	}
	if !strings.Contains(strings.ToLower(out.String()), "dirty") {
		t.Errorf("the skipped rebase should be reported as a dirty tree, got: %q", out.String())
	}
	if fake.CountOp("create") != 1 {
		t.Errorf("a dirty tree still continues to open the PR")
	}
	if !r.Dirty() {
		t.Errorf("the uncommitted change should be left intact")
	}
}

// More than one commit ahead of origin/main is refused with the squash remedy.
func TestEnforcesOneCommitPerPR(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	r.CommitFile("one.txt", "1\n", "feat(f): first commit on the feature branch for the count rule")
	r.CommitFile("two.txt", "2\n", "feat(f): second commit that violates the one commit per pr rule")

	fake := gittest.NewFakeGH()
	err := parity.Run(deps(r, fake, &bytes.Buffer{}))
	if err == nil {
		t.Fatalf("two commits ahead of main should be refused")
	}
	if !strings.Contains(err.Error(), "git reset --soft origin/main && git commit") {
		t.Errorf("the one-commit refusal should print the squash remedy, got: %q", err.Error())
	}
	if fake.CountOp("create") != 0 {
		t.Errorf("no PR should be opened when the one-commit rule fails")
	}
}

// The PR is created with the commit subject as title and the commit body as
// body, targeting main, with auto-merge armed.
func TestCreatesPRWithCommitSubjectAndBody(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	subject := "feat(f): add the single feature commit with a descriptive body below"
	body := "why this change matters and how it was verified end to end\n\n- one bullet describing the change and its reason"
	r.WriteFile("f.txt", "f\n")
	r.Add("f.txt")
	r.Git("commit", "-m", subject, "-m", body)

	fake := gittest.NewFakeGH()
	if err := parity.Run(deps(r, fake, &bytes.Buffer{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	w, ok := fake.Find("create", "feat")
	if !ok {
		t.Fatalf("expected a PR to be created for feat\n%s", fake)
	}
	if w.Base != "main" {
		t.Errorf("single PR base = %q, want main", w.Base)
	}
	if w.Title != subject {
		t.Errorf("PR title = %q, want the commit subject %q", w.Title, subject)
	}
	if !strings.Contains(w.Body, "why this change matters") || !strings.Contains(w.Body, "- one bullet") {
		t.Errorf("PR body should carry the commit body, got: %q", w.Body)
	}
	if _, ok := fake.Find("automerge", "feat"); !ok {
		t.Errorf("auto-merge should be armed for the single PR\n%s", fake)
	}
	if _, ok := gittest.RemoteSHA(t, origin, "refs/heads/feat"); !ok {
		t.Errorf("feat should have been pushed to origin")
	}
}

// When a PR already exists, pr.sh re-syncs it via edit after an amend instead of
// creating a second one.
func TestReSyncsExistingPRWithEdit(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	subject := "feat(f): amended feature commit whose pull request already exists"
	r.CommitFile("f.txt", "f\n", subject)

	fake := gittest.NewFakeGH()
	fake.Existing["feat"] = true
	if err := parity.Run(deps(r, fake, &bytes.Buffer{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.CountOp("create") != 0 {
		t.Errorf("an existing PR must be edited, not re-created")
	}
	w, ok := fake.Find("edit", "feat")
	if !ok {
		t.Fatalf("existing PR should be re-synced via edit\n%s", fake)
	}
	if w.Title != subject {
		t.Errorf("edited PR title = %q, want %q", w.Title, subject)
	}
}

// When arming auto-merge is rejected (checks still pending), fall back to
// watching checks and merging on green.
func TestAutoMergeFallsBackToWatchingChecks(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	r.Git("checkout", "-b", "feat")
	r.CommitFile("f.txt", "f\n", "feat(f): feature commit exercising the auto merge arm fallback path")

	fake := gittest.NewFakeGH()
	fake.AutoMergeErr = errors.New("Pull request is not in a mergeable state; checks are pending")
	if err := parity.Run(deps(r, fake, &bytes.Buffer{})); err != nil {
		t.Fatalf("Run should still succeed via the checks-watch fallback: %v", err)
	}
	if !opsInOrder(fake.Ops(), "automerge", "watch", "merge") {
		t.Errorf("fallback should arm auto-merge, then watch checks, then merge; got %v", fake.Ops())
	}
	if fake.CountOp("watch") != 1 || fake.CountOp("merge") != 1 {
		t.Errorf("the fallback should watch once and merge once; got %v", fake.Ops())
	}
}

// opsInOrder reports whether want appears as an ordered (not necessarily
// contiguous) subsequence of ops.
func opsInOrder(ops []string, want ...string) bool {
	i := 0
	for _, op := range ops {
		if i < len(want) && op == want[i] {
			i++
		}
	}
	return i == len(want)
}
