// Package warn implements the 150-line size warning. It never blocks a commit
// or a push; it only prints a heads-up written for the author - human or agent -
// with the split remedy inline.
//
// The count is the added plus deleted lines of a commit's diff, excluding paths
// that match the exempt regex. The exempt set mirrors the EXEMPT pattern in
// scripts/check-commit-msg.sh (flexinference-mono): lockfiles, generated SDK and
// contract files, translated docs, and message catalogs - files no human
// hand-authored. That regex must live in exactly one place in this package,
// with a comment naming scripts/check-commit-msg.sh as its source, so the two
// stay in sync.
//
// The warning fires in two places: after a `floppygit commit` (a passthrough to
// git commit that then prints the summary) and as a per-level summary during
// `floppygit push`. Example copy:
//
//	212 authored lines in this commit; consider splitting -
//	floppygit new <name> starts a stacked commit for the next unit.
package warn
