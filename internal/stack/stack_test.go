package stack_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
	"github.com/aperswal/floppygit-mono/internal/gittest"
	"github.com/aperswal/floppygit-mono/internal/stack"
)

// The fake gh runner must satisfy the real gh seam; if the interface and the
// fake ever drift, this stops compiling.
var _ gitexec.GH = (*gittest.FakeGH)(nil)

func deps(r *gittest.Repo, fake *gittest.FakeGH, out *bytes.Buffer) stack.Deps {
	return stack.Deps{Git: gitexec.Git{Dir: r.Dir}, GH: fake, Out: out}
}

// new branches off the current tip without adding a commit and records the
// current branch as the new branch's floppyParent.
func TestNewBranchesOffTipAndRecordsParent(t *testing.T) {
	r := gittest.NewRepo(t)
	tip := r.Head()
	d := deps(r, gittest.NewFakeGH(), &bytes.Buffer{})

	if err := stack.New(d, "feature-a"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Branch() != "feature-a" {
		t.Errorf("after New, current branch = %q, want feature-a", r.Branch())
	}
	if r.Head() != tip {
		t.Errorf("New added a commit; tip moved %s -> %s", tip, r.Head())
	}
	if got := r.FloppyParent("feature-a"); got != "main" {
		t.Errorf("floppyParent = %q, want main (the branch New was run from)", got)
	}
}

// A three-deep stack records the parent chain so push can walk it from the root.
func TestNewRecordsFullParentChain(t *testing.T) {
	r := gittest.NewRepo(t)
	d := deps(r, gittest.NewFakeGH(), &bytes.Buffer{})

	if err := stack.New(d, "a"); err != nil {
		t.Fatalf("New a: %v", err)
	}
	r.CommitFile("a.txt", "a\n", "feat(a): add file a as the first unit of the stack")
	if err := stack.New(d, "b"); err != nil {
		t.Fatalf("New b: %v", err)
	}
	r.CommitFile("b.txt", "b\n", "feat(b): add file b as the second unit of the stack")
	if err := stack.New(d, "c"); err != nil {
		t.Fatalf("New c: %v", err)
	}

	for _, tc := range []struct{ branch, parent string }{
		{"a", "main"},
		{"b", "a"},
		{"c", "b"},
	} {
		if got := r.FloppyParent(tc.branch); got != tc.parent {
			t.Errorf("floppyParent(%s) = %q, want %q", tc.branch, got, tc.parent)
		}
	}
}

// push walks the chain bottom-to-top, opening one PR per level whose base is the
// branch below (main at the root), and arms auto-merge on the bottom PR only.
func TestPushWalksBottomUpAndArmsBottomOnly(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	fake := gittest.NewFakeGH()
	d := deps(r, fake, &bytes.Buffer{})

	subjA := "feat(a): add file a to the repository as the stack bottom"
	subjB := "feat(b): add file b stacked directly on top of file a"
	subjC := "feat(c): add file c stacked directly on top of file b"
	mustNew(t, d, "a")
	r.CommitFile("a.txt", "a\n", subjA)
	mustNew(t, d, "b")
	r.CommitFile("b.txt", "b\n", subjB)
	mustNew(t, d, "c")
	r.CommitFile("c.txt", "c\n", subjC)

	if err := stack.Push(d); err != nil {
		t.Fatalf("Push: %v\ncalls:\n%s", err, fake)
	}

	writes := fake.Writes()
	if len(writes) != 3 {
		t.Fatalf("expected 3 PR create/edit calls, got %d:\n%s", len(writes), fake)
	}
	wantBranch := []string{"a", "b", "c"}
	wantBase := []string{"main", "a", "b"}
	wantTitle := []string{subjA, subjB, subjC}
	for i, w := range writes {
		if w.Branch != wantBranch[i] || w.Base != wantBase[i] {
			t.Errorf("write %d = %s -> base %s, want %s -> base %s", i, w.Branch, w.Base, wantBranch[i], wantBase[i])
		}
		if w.Title != wantTitle[i] {
			t.Errorf("write %d title = %q, want %q (the commit subject)", i, w.Title, wantTitle[i])
		}
	}

	if n := fake.CountOp("automerge"); n != 1 {
		t.Errorf("auto-merge armed %d times, want exactly 1 (bottom only)\n%s", n, fake)
	}
	if _, ok := fake.Find("automerge", "a"); !ok {
		t.Errorf("auto-merge must be armed on the bottom PR (a), not elsewhere\n%s", fake)
	}

	for _, b := range []string{"a", "b", "c"} {
		if _, ok := gittest.RemoteSHA(t, origin, "refs/heads/"+b); !ok {
			t.Errorf("branch %s was not pushed to origin", b)
		}
	}
}

// During push each level prints its size summary; a level over 150 authored
// lines shows the split remedy.
func TestPushPrintsPerLevelSizeSummary(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	fake := gittest.NewFakeGH()
	var out bytes.Buffer
	d := deps(r, fake, &out)

	mustNew(t, d, "big")
	r.CommitFile("big.ts", strings.Repeat("const line = true;\n", 200),
		"feat(big): add a large module that trips the size warning here")

	if err := stack.Push(d); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !strings.Contains(out.String(), "authored lines in this commit") ||
		!strings.Contains(out.String(), "floppygit new") {
		t.Errorf("push should print the per-level size warning, got:\n%s", out.String())
	}
}

// fix folds a working-tree edit into the commit that owns it, carries every
// branch above along, leaves no fixup commit, and force-pushes the whole chain.
func TestFixAbsorbsEditRestacksAndPushesChain(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	fake := gittest.NewFakeGH()
	d := deps(r, fake, &bytes.Buffer{})

	mustNew(t, d, "a")
	r.CommitFile("feature.go", "package a\n\nfunc A() {}\n",
		"feat(a): add the A function to the feature package as the base")
	mustNew(t, d, "b")
	r.CommitFile("other.go", "package a\n\nfunc B() {}\n",
		"feat(b): add the B function stacked on top of the A change")
	r.Git("push", "origin", "a", "b")
	aBefore := r.RevParse("a")

	// The reviewer flags A; edit the file in place and fix the a commit.
	r.WriteFile("feature.go", "package a\n\nfunc A() int { return 1 }\n")
	if err := stack.Fix(d, "a"); err != nil {
		t.Fatalf("Fix: %v", err)
	}

	if r.Dirty() {
		t.Errorf("working tree should be clean after fix:\n%s", r.Git("status", "--porcelain"))
	}
	if r.CountRange("main..a") != 1 || r.CountRange("a..b") != 1 {
		t.Errorf("each level must still hold exactly one commit after autosquash")
	}
	for _, s := range r.Subjects("main..b") {
		if strings.HasPrefix(s, "fixup!") {
			t.Errorf("a fixup commit was left behind: %q", s)
		}
	}
	if got := r.FileAt("a", "feature.go"); !strings.Contains(got, "return 1") {
		t.Errorf("the edit was not folded into commit a; a:feature.go =\n%s", got)
	}
	aAfter := r.RevParse("a")
	if aAfter == aBefore {
		t.Errorf("commit a should have been rewritten by the fixup")
	}
	if sha, _ := gittest.RemoteSHA(t, origin, "refs/heads/a"); sha != aAfter {
		t.Errorf("origin/a not force-updated to the rewritten commit")
	}
	if sha, _ := gittest.RemoteSHA(t, origin, "refs/heads/b"); sha != r.RevParse("b") {
		t.Errorf("origin/b not force-updated after the restack")
	}
}

// sync restacks onto origin/main; a merged one-commit PR is patch-identical, so
// the rebase drops it, the merged branch is deleted, the next branch becomes the
// bottom, and auto-merge re-arms on it.
func TestSyncDropsMergedCommitPromotesBottomAndRearms(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	fake := gittest.NewFakeGH()
	d := deps(r, fake, &bytes.Buffer{})

	mustNew(t, d, "a")
	r.CommitFile("a.txt", "a\n", "feat(a): add file a as the bottom of the stack now")
	mustNew(t, d, "b")
	r.CommitFile("b.txt", "b\n", "feat(b): add file b stacked directly on top of a")
	aSHA := r.RevParse("a")
	r.Git("push", "origin", "a", "b")

	// a is squash-merged: a patch-identical commit lands on origin/main.
	gittest.SquashMergeToOrigin(t, origin, aSHA, "feat(a): squashed onto main by the merge")

	if err := stack.Sync(d); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if r.BranchExists("a") {
		t.Errorf("fully-merged branch a should have been deleted")
	}
	if !r.IsAncestor("origin/main", "b") {
		t.Errorf("b was not restacked onto origin/main")
	}
	if r.CountRange("origin/main..b") != 1 {
		t.Errorf("b should be exactly one commit ahead of the new origin/main")
	}
	if got := r.FloppyParent("b"); got != "main" {
		t.Errorf("b should be promoted to the root; floppyParent = %q, want main", got)
	}
	if _, ok := fake.Find("automerge", "b"); !ok {
		t.Errorf("auto-merge must re-arm on the new bottom b\n%s", fake)
	}
}

// Any restack conflict stops loudly with the resolution commands and never
// leaves the stack half-moved.
func TestSyncConflictStopsLoudlyWithoutHalfRestack(t *testing.T) {
	origin := gittest.NewOrigin(t)
	r := gittest.Clone(t, origin)
	fake := gittest.NewFakeGH()
	d := deps(r, fake, &bytes.Buffer{})

	mustNew(t, d, "a")
	r.WriteFile("base.txt", "alpha\n")
	r.Add("base.txt")
	r.Git("commit", "-m", "feat(a): change base to alpha as the stack bottom edit")
	aBefore := r.RevParse("a")

	// origin/main changes the same lines a touched: the restack will conflict.
	gittest.AdvanceOriginMain(t, origin, "base.txt", "origin-side\n",
		"ops(main): change base to a conflicting value on origin main now")

	err := stack.Sync(d)
	if err == nil {
		t.Fatalf("Sync should stop on a restack conflict")
	}
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "conflict") {
		t.Errorf("error should name the conflict, got: %q", msg)
	}
	for _, sub := range []string{"git rebase", "--continue"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("error should print the resolution command %q, got: %q", sub, msg)
		}
	}

	if r.RebaseInProgress() {
		t.Errorf("sync left a rebase in progress: half-restacked state")
	}
	if r.RevParse("a") != aBefore {
		t.Errorf("branch a moved despite the conflict: %s -> %s", aBefore, r.RevParse("a"))
	}
}

func mustNew(t *testing.T, d stack.Deps, name string) {
	t.Helper()
	if err := stack.New(d, name); err != nil {
		t.Fatalf("New %s: %v", name, err)
	}
}
