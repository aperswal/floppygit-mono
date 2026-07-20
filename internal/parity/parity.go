package parity

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
)

// Deps are the collaborators the single-PR path needs: the git handle, the gh
// seam, and where to write status.
type Deps struct {
	Git gitexec.Git
	GH  gitexec.GH
	Out io.Writer
}

// Run reproduces scripts/pr.sh from the flexinference-mono repo: the single-PR
// path floppygit falls back to when the current branch is not part of a stack.
// It is a faithful port; see internal/parity/doc.go for the behavior list and
// scripts/pr.sh for the source of truth.
func Run(d Deps) error {
	g := d.Git

	branch := currentBranch(g)
	if branch == "" || branch == "main" {
		return gitexec.UserFault("floppygit: run this from a feature branch, not main (git switch -c <name>)")
	}

	if _, err := g.Output("fetch", "--prune", "origin"); err != nil {
		return gitexec.EnvFault(fmt.Sprintf("floppygit: git fetch failed: %v", err))
	}

	if err := pruneGoneBranches(d, branch); err != nil {
		return err
	}

	if err := syncWithMain(d); err != nil {
		return err
	}

	count, err := commitCount(g, "origin/main", "HEAD")
	if err != nil {
		return err
	}
	if count != 1 {
		return gitexec.UserFault(fmt.Sprintf(
			"floppygit: branch has %d commits ahead of origin/main; the rule is one.\n"+
				"  squash: git reset --soft origin/main && git commit   (then rerun)", count))
	}

	if err := g.Run("push", "--force-with-lease", "-u", "origin", branch); err != nil {
		return gitexec.UserFault(fmt.Sprintf("floppygit: pushing %s failed: %v", branch, err))
	}

	title, _ := g.Output("log", "-1", "--pretty=%s")
	body, _ := g.Output("log", "-1", "--pretty=%b")
	exists, err := d.GH.ViewPR(branch)
	if err != nil {
		return err
	}
	if exists {
		if err := d.GH.EditPR(branch, "main", title, body); err != nil {
			return err
		}
		fmt.Fprintln(d.Out, "existing PR re-synced to the commit message")
	} else {
		if err := d.GH.CreatePR(branch, "main", title, body); err != nil {
			return err
		}
	}

	return armAutoMerge(d, branch)
}

// pruneGoneBranches deletes local branches whose upstream is gone (a squash
// merge deletes the remote branch, leaving the upstream [gone]). The current
// branch and any branch with no upstream are never touched.
func pruneGoneBranches(d Deps, current string) error {
	out, err := d.Git.Output("branch", "--format", "%(refname:short) %(upstream:track)")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "[gone]" {
			continue
		}
		gone := fields[0]
		if gone == current {
			continue
		}
		if _, err := d.Git.Output("branch", "-D", gone); err != nil {
			return fmt.Errorf("pruning %s: %w", gone, err)
		}
		fmt.Fprintf(d.Out, "pruned merged branch %s\n", gone)
	}
	return nil
}

// syncWithMain keeps the branch current with origin/main: a conflict is a hard
// stop with the rebase remedy, a clean tree behind main is rebased, and a dirty
// but conflict-free tree warns and continues (the squash merge stays clean).
func syncWithMain(d Deps) error {
	g := d.Git

	// Already contains origin/main: nothing to do.
	if _, err := g.Output("merge-base", "--is-ancestor", "origin/main", "HEAD"); err == nil {
		return nil
	}

	// merge-tree detects a conflict against the latest origin/main without
	// touching the working tree, so this works even mid-flight.
	if _, err := g.Output("merge-tree", "--write-tree", "origin/main", "HEAD"); err != nil {
		return gitexec.UserFault(
			"floppygit: branch conflicts with the latest origin/main; fix locally first:\n" +
				"  git rebase origin/main   (resolve, git rebase --continue, then rerun)")
	}

	if treeClean(g) {
		if _, err := g.Output("rebase", "origin/main"); err != nil {
			return gitexec.UserFault(fmt.Sprintf("floppygit: rebase onto origin/main failed; resolve and rerun: %v", err))
		}
		fmt.Fprintln(d.Out, "rebased onto the latest origin/main")
		return nil
	}

	fmt.Fprintln(d.Out, "branch is behind origin/main but conflict-free; dirty tree, skipping local rebase (the squash merge stays clean)")
	return nil
}

// armAutoMerge arms auto-merge; if GitHub rejects arming while checks are still
// pending, it falls back to watching checks and merging on green.
func armAutoMerge(d Deps, branch string) error {
	if err := d.GH.AutoMerge(branch); err != nil {
		fmt.Fprintln(d.Out, "auto-merge arm rejected; watching checks instead")
		if err := d.GH.WatchChecks(branch); err != nil {
			return err
		}
		return d.GH.Merge(branch)
	}
	return nil
}

func currentBranch(g gitexec.Git) string {
	out, _ := g.Output("branch", "--show-current")
	return strings.TrimSpace(out)
}

func treeClean(g gitexec.Git) bool {
	if _, err := g.Output("diff", "--quiet"); err != nil {
		return false
	}
	if _, err := g.Output("diff", "--cached", "--quiet"); err != nil {
		return false
	}
	return true
}

func commitCount(g gitexec.Git, base, ref string) (int, error) {
	out, err := g.Output("rev-list", "--count", base+".."+ref)
	if err != nil {
		return 0, gitexec.UserFault(fmt.Sprintf("counting commits %s..%s: %v", base, ref, err))
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parsing commit count %q: %w", out, err)
	}
	return n, nil
}
