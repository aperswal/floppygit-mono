// Package stack implements floppygit's stacking commands: new, push, fix, and
// sync. A stack is a chain of one-commit branches, each recording its parent in
// git config as branch.<name>.floppyParent, that becomes a chain of one-commit
// pull requests merging bottom-up.
//
// Commands:
//
//	new <name>    Branch off the current tip and record <name>'s parent as the
//	              current branch, so the chain can be walked from its root later.
//
//	push          Walk the chain from its root to its tip. Per level: validate
//	              the one-commit rule, print the 150-line size summary (see
//	              internal/warn), push --force-with-lease, and create or update
//	              the level's PR with base = the parent branch, or main at the
//	              root. Auto-merge (squash) is armed on the bottom PR only, so
//	              the stack lands in order.
//
//	fix <ref>     Stage the working changes as a --fixup of the commit named by
//	              <ref> (a SHA or a branch tip), fold them into place with a
//	              non-interactive autosquash rebase from the stack root
//	              (GIT_SEQUENCE_EDITOR=true git rebase -i --autosquash
//	              --update-refs) so every branch above moves with the edit, and
//	              push the whole chain --force-with-lease.
//
//	sync          Fetch, then restack the chain onto origin/main. A merged
//	              one-commit PR is patch-identical to its local commit, so the
//	              rebase drops it automatically - no double-applied commits.
//	              Delete fully-merged local branches and re-arm auto-merge on the
//	              new bottom of the stack.
//
// Any restack that hits a conflict - in fix or in sync - stops loudly and
// prints the exact commands to resolve and continue. floppygit never leaves a
// half-restacked stack behind.
package stack
