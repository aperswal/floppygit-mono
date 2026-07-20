// Package gitexec runs every git and gh operation by shelling out to the real
// system binaries. floppygit never reimplements git: commit-msg and pre-push
// hooks, credential helpers, and local config all fire exactly as they do under
// plain git because every command floppygit runs is the actual git command.
//
// This package is the single choke point for spawning subprocesses. It is
// expected to expose:
//
//   - a way to run git with inherited stdio, so hooks, editors, and auth
//     prompts reach the terminal unchanged;
//   - a way to run git and capture its output for parsing (current branch,
//     commit counts, config values, diff numstat);
//   - a thin gh interface (create PR, edit PR, arm auto-merge, watch checks)
//     that tests fake, so PR and auto-merge behavior can be exercised against
//     throwaway local repos with no network and no real GitHub.
//
// Errors distinguish an environment fault (git or gh missing) from a
// user-fixable one (dirty tree, conflict) via the exit code floppygit returns.
package gitexec
