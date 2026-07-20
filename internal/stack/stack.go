package stack

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
	"github.com/aperswal/floppygit-mono/internal/warn"
)

// Deps are the collaborators every stack command needs: the git handle, the gh
// seam, and where to write status and size summaries.
type Deps struct {
	Git gitexec.Git
	GH  gitexec.GH
	Out io.Writer
}

// New branches off the current tip and records the current branch as the new
// branch's floppyParent, so the chain can be walked from its root later. The
// root of a stack has floppyParent "main".
func New(d Deps, name string) error {
	if name == "" {
		return gitexec.UserFault("floppygit new: a branch name is required")
	}
	parent := currentBranch(d.Git)
	if parent == "" {
		return gitexec.UserFault("floppygit new: HEAD is detached; check out a branch first")
	}
	if _, err := d.Git.Output("checkout", "-b", name); err != nil {
		return gitexec.UserFault(fmt.Sprintf("floppygit new: could not create branch %q: %v", name, err))
	}
	if _, err := d.Git.Output("config", "--local", "branch."+name+".floppyParent", parent); err != nil {
		return fmt.Errorf("recording stack parent for %s: %w", name, err)
	}
	fmt.Fprintf(d.Out, "started %s on top of %s\n", name, parent)
	return nil
}

// Push walks the stack from its root to the current branch, pushing each level
// and opening or updating its one-commit PR (base = the parent branch, or main
// at the root), then arms auto-merge on the bottom PR only.
func Push(d Deps) error {
	chain, err := currentChain(d.Git, "push")
	if err != nil {
		return err
	}
	return publishChain(d, chain)
}

// Fix folds a working-tree edit into the commit that owns it, carries every
// branch above along with --update-refs, and force-pushes the whole chain. ref
// is the commit SHA or the branch tip the change belongs to.
func Fix(d Deps, ref string) error {
	if ref == "" {
		return gitexec.UserFault("floppygit fix: name the commit or branch the change belongs to")
	}
	chain, err := currentChain(d.Git, "fix")
	if err != nil {
		return err
	}
	base := rebaseBase(d.Git, chain[0])

	target, err := d.Git.Output("rev-parse", ref)
	if err != nil {
		return gitexec.UserFault(fmt.Sprintf("floppygit fix: %q is not a commit or branch", ref))
	}
	tipBefore, err := d.Git.Output("rev-parse", "HEAD")
	if err != nil {
		return err
	}

	// Stage the working changes and record them as a fixup of the target commit.
	if _, err := d.Git.Output("add", "-A"); err != nil {
		return gitexec.UserFault(fmt.Sprintf("floppygit fix: staging the edit failed: %v", err))
	}
	if err := d.Git.Run("commit", "--fixup="+target); err != nil {
		return gitexec.UserFault("floppygit fix: recording the fixup commit failed (nothing to fix, or a rejecting hook)")
	}

	// Fold it into place non-interactively, moving every branch above with it.
	reb := d.Git
	reb.Env = []string{"GIT_SEQUENCE_EDITOR=true", "GIT_EDITOR=true"}
	if _, err := reb.Output("rebase", "-i", "--autosquash", "--update-refs", base); err != nil {
		// Never leave a half-restacked stack behind: abort and unwind the fixup,
		// leaving the edit staged again exactly as the user had it.
		_, _ = d.Git.Output("rebase", "--abort")
		_, _ = d.Git.Output("reset", "--soft", tipBefore)
		return gitexec.UserFault(
			"floppygit fix: the autosquash rebase hit a conflict and was rolled back; nothing was pushed.\n" +
				"  your edit is staged again. resolve by hand:\n" +
				"    git rebase -i --autosquash --update-refs " + base + "\n" +
				"    (fix the conflicts, then: git rebase --continue)")
	}

	if err := pushChain(d, chain); err != nil {
		return err
	}
	fmt.Fprintf(d.Out, "folded the edit into %s and restacked %d branch(es)\n", target[:min(7, len(target))], len(chain))
	return nil
}

// Sync fetches, restacks the chain onto origin/main (a merged one-commit PR is
// patch-identical, so the rebase drops it), deletes fully-merged local branches,
// promotes the new bottom to the root, and re-publishes the surviving chain so
// auto-merge re-arms on the new bottom. A conflict stops loudly and leaves the
// stack exactly where it was.
func Sync(d Deps) error {
	chain, err := currentChain(d.Git, "sync")
	if err != nil {
		return err
	}
	if _, err := d.Git.Output("fetch", "--prune", "origin"); err != nil {
		return gitexec.EnvFault(fmt.Sprintf("floppygit sync: git fetch failed: %v", err))
	}

	reb := d.Git
	reb.Env = []string{"GIT_SEQUENCE_EDITOR=true", "GIT_EDITOR=true"}
	if _, err := reb.Output("rebase", "--update-refs", "origin/main"); err != nil {
		_, _ = d.Git.Output("rebase", "--abort")
		return gitexec.UserFault(
			"floppygit sync: restacking onto origin/main hit a conflict; nothing was moved.\n" +
				"  resolve by hand:\n" +
				"    git rebase origin/main\n" +
				"    (fix the conflicts, then: git rebase --continue, and rerun floppygit sync)")
	}

	// Split the chain into branches whose one commit already landed on
	// origin/main and those still live, keeping order so the lowest live branch
	// becomes the new bottom. A squash-merged commit is patch-identical but has a
	// different SHA, and the restack leaves its branch ref stale rather than
	// reachable, so merged-ness is a patch-id question (git cherry), not commit
	// reachability.
	var survivors, merged []string
	for _, branch := range chain {
		m, err := isMerged(d.Git, branch)
		if err != nil {
			return err
		}
		if m {
			merged = append(merged, branch)
		} else {
			survivors = append(survivors, branch)
		}
	}

	// If every branch merged, step onto main so the whole stack can be deleted.
	if len(survivors) == 0 {
		if _, err := d.Git.Output("checkout", "main"); err != nil {
			return gitexec.UserFault(fmt.Sprintf("floppygit sync: switching to main failed: %v", err))
		}
	}
	for _, branch := range merged {
		if branch == currentBranch(d.Git) {
			continue // never delete the branch we are standing on
		}
		if _, err := d.Git.Output("branch", "-D", branch); err != nil {
			return fmt.Errorf("deleting merged branch %s: %w", branch, err)
		}
		fmt.Fprintf(d.Out, "deleted merged branch %s\n", branch)
	}
	if len(survivors) == 0 {
		fmt.Fprintln(d.Out, "the whole stack has merged; nothing left to sync")
		return nil
	}

	// Promote the new bottom to the root: its parent is now main.
	if _, err := d.Git.Output("config", "--local", "branch."+survivors[0]+".floppyParent", "main"); err != nil {
		return fmt.Errorf("promoting %s to the stack root: %w", survivors[0], err)
	}
	return publishChain(d, survivors)
}

// publishChain pushes each level bottom-up, opens or updates its PR, prints its
// size summary, and arms auto-merge on the bottom (root) PR only.
func publishChain(d Deps, chain []string) error {
	for _, branch := range chain {
		base := parentOf(d.Git, branch)

		count, err := commitCount(d.Git, countBase(base), branch)
		if err != nil {
			return err
		}
		if count != 1 {
			return gitexec.UserFault(fmt.Sprintf(
				"floppygit push: %s has %d commits over %s; the rule is one per level.\n"+
					"  squash: git reset --soft %s && git commit   (then rerun)",
				branch, count, base, countBase(base)))
		}

		if n, err := authoredCount(d.Git, branch); err == nil {
			fmt.Fprintf(d.Out, "%s: %d authored line(s)\n", branch, n)
			if w := warn.Warning(n); w != "" {
				fmt.Fprintln(d.Out, w)
			}
		}

		if err := d.Git.Run("push", "--force-with-lease", "-u", "origin", branch); err != nil {
			return gitexec.UserFault(fmt.Sprintf("floppygit push: pushing %s failed: %v", branch, err))
		}

		title, _ := d.Git.Output("log", "-1", "--pretty=%s", branch)
		body, _ := d.Git.Output("log", "-1", "--pretty=%b", branch)
		exists, err := d.GH.ViewPR(branch)
		if err != nil {
			return err
		}
		if exists {
			if err := d.GH.EditPR(branch, base, title, body); err != nil {
				return err
			}
		} else {
			if err := d.GH.CreatePR(branch, base, title, body); err != nil {
				return err
			}
		}
	}
	return armAutoMerge(d, chain[0])
}

// pushChain force-pushes every branch in the chain, used after a fix restack;
// the PRs update themselves from the new commits.
func pushChain(d Deps, chain []string) error {
	for _, branch := range chain {
		if err := d.Git.Run("push", "--force-with-lease", "-u", "origin", branch); err != nil {
			return gitexec.UserFault(fmt.Sprintf("floppygit fix: pushing %s failed: %v", branch, err))
		}
	}
	return nil
}

// armAutoMerge arms auto-merge on the bottom PR; if GitHub rejects arming while
// checks are still pending, it falls back to watching checks and merging.
func armAutoMerge(d Deps, bottom string) error {
	if err := d.GH.AutoMerge(bottom); err != nil {
		fmt.Fprintf(d.Out, "auto-merge arm rejected for %s; watching checks instead\n", bottom)
		if err := d.GH.WatchChecks(bottom); err != nil {
			return err
		}
		return d.GH.Merge(bottom)
	}
	return nil
}

// currentChain returns the stack from its root to the current branch, refusing
// on main or a detached HEAD.
func currentChain(g gitexec.Git, cmd string) ([]string, error) {
	tip := currentBranch(g)
	if tip == "" {
		return nil, gitexec.UserFault("floppygit " + cmd + ": HEAD is detached; check out a stacked branch")
	}
	if tip == "main" {
		return nil, gitexec.UserFault("floppygit " + cmd + ": run this from a stacked feature branch, not main")
	}
	chain := chainFromRoot(g, tip)
	if len(chain) == 0 {
		return nil, gitexec.UserFault("floppygit " + cmd + ": no stacked branch here; floppygit new <name> first")
	}
	return chain, nil
}

// chainFromRoot walks floppyParent links from tip up to the root, returning the
// branches root-first.
func chainFromRoot(g gitexec.Git, tip string) []string {
	var chain []string
	cur := tip
	for cur != "" && cur != "main" {
		chain = append([]string{cur}, chain...)
		parent := parentOf(g, cur)
		if parent == "" {
			break
		}
		cur = parent
	}
	return chain
}

func currentBranch(g gitexec.Git) string {
	out, _ := g.Output("branch", "--show-current")
	return strings.TrimSpace(out)
}

func parentOf(g gitexec.Git, branch string) string {
	out, _ := g.Output("config", "--local", "--get", "branch."+branch+".floppyParent")
	return strings.TrimSpace(out)
}

// rebaseBase is the ref an autosquash rebase replays onto: the root's parent, or
// main when the root sits directly on main.
func rebaseBase(g gitexec.Git, root string) string {
	base := parentOf(g, root)
	if base == "" {
		return "main"
	}
	return base
}

// countBase maps a PR base to the ref the one-commit rule counts against. The
// root's base is the branch name "main", but the count must be against
// origin/main, since local main can be stale after other PRs merged.
func countBase(base string) string {
	if base == "main" {
		return "origin/main"
	}
	return base
}

func commitCount(g gitexec.Git, base, branch string) (int, error) {
	out, err := g.Output("rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, gitexec.UserFault(fmt.Sprintf("counting commits %s..%s: %v", base, branch, err))
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parsing commit count %q: %w", out, err)
	}
	return n, nil
}

// isMerged reports whether a branch's commits are all already on origin/main by
// patch id. git cherry prefixes a commit "-" when an equivalent change is
// upstream and "+" when it is not, so a branch with no "+" line is fully merged.
// This holds even when the squash landed under a different SHA, which plain
// commit-reachability (origin/main..branch) would miss.
func isMerged(g gitexec.Git, branch string) (bool, error) {
	out, err := g.Output("cherry", "origin/main", branch)
	if err != nil {
		return false, gitexec.UserFault(fmt.Sprintf("checking whether %s is merged: %v", branch, err))
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			return false, nil
		}
	}
	return true, nil
}

func authoredCount(g gitexec.Git, ref string) (int, error) {
	raw, err := g.Output("diff-tree", "--no-commit-id", "--numstat", "-r", "--root", ref)
	if err != nil {
		return 0, err
	}
	return warn.CountAuthored(warn.ParseNumstat(raw)), nil
}
