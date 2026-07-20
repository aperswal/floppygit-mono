// Package parity reproduces scripts/pr.sh from the flexinference-mono repo as
// floppygit's single-PR path: the behavior floppygit falls back to when the
// current branch is not part of a stack.
//
// Source of truth: scripts/pr.sh. The behaviors mirrored here, in order:
//
//   - refuse to run on main or a detached HEAD;
//   - fetch --prune, then delete local branches whose upstream is gone. A
//     squash-merged PR deletes its remote branch, so git branch --merged can
//     never see it; the reliable signal is the upstream going [gone]. Only
//     branches that had an upstream and lost it are deleted, never the current
//     branch and never unpushed local work;
//   - stay current with origin/main using git merge-tree to detect conflicts
//     without touching the working tree. A real conflict is a hard stop with
//     the printed "git rebase origin/main" remedy; a clean tree behind main is
//     rebased automatically; a dirty but conflict-free tree warns and continues
//     (the squash merge stays clean regardless);
//   - enforce exactly one commit ahead of origin/main, printing the
//     "git reset --soft origin/main && git commit" squash remedy otherwise;
//   - push --force-with-lease, so an amend-and-rerun just works;
//   - gh pr create with the commit subject as title and the body as body, or
//     gh pr edit to re-sync an existing PR after an amend;
//   - gh pr merge --auto --squash --delete-branch, falling back to watching
//     checks and merging on green when arming auto-merge is rejected while
//     checks are still pending.
package parity
