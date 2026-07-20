package warn_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
	"github.com/aperswal/floppygit-mono/internal/gittest"
	"github.com/aperswal/floppygit-mono/internal/warn"
)

// The exempt set must mirror the EXEMPT regex in scripts/check-commit-msg.sh of
// flexinference-mono: lockfiles, generated SDK/contract files, translated docs,
// and message catalogs are excluded from the authored-line count; hand-authored
// source and copy (including en.json and untranslated docs) are counted.
func TestIsExempt(t *testing.T) {
	cases := []struct {
		path   string
		exempt bool
	}{
		// lockfiles, anywhere in the tree
		{"pnpm-lock.yaml", true},
		{"apps/web/pnpm-lock.yaml", true},
		{"package-lock.json", true},
		{"uv.lock", true},
		// translated docs and the exempt faq source
		{"docs/de/guide.mdx", true},
		{"docs/zh/pricing.mdx", true},
		{"docs/faq.mdx", true},
		// generated SDK / contract / pricing artifacts
		{"packages/sdk-typescript/src/types.ts", true},
		{"packages/sdk-python/src/flexinference/models.py", true},
		{"contract/flexinference-openapi.yaml", true},
		{"packages/core/src/pricing-data.generated.ts", true},
		{"harness/matrix.smoke.json", true},
		// generated message catalogs and their hash manifest
		{"apps/website/messages/de.json", true},
		{"apps/website/messages/.hashes.json", true},
		// hand-authored: source, config, untranslated docs, and en.json copy
		{"src/index.ts", false},
		{"cmd/floppygit/main.go", false},
		{"apps/website/messages/en.json", false},
		{"docs/guide.mdx", false},
		{"README.md", false},
		{"logo.png", false},
	}
	for _, c := range cases {
		if got := warn.IsExempt(c.path); got != c.exempt {
			t.Errorf("IsExempt(%q) = %v, want %v", c.path, got, c.exempt)
		}
	}
}

func TestParseNumstat(t *testing.T) {
	raw := "10\t5\tsrc/a.ts\n0\t3\tpnpm-lock.yaml\n-\t-\tlogo.png\n"
	got := warn.ParseNumstat(raw)
	want := []warn.NumstatEntry{
		{Added: 10, Deleted: 5, Path: "src/a.ts"},
		{Added: 0, Deleted: 3, Path: "pnpm-lock.yaml"},
		{Added: 0, Deleted: 0, Path: "logo.png"}, // binary "-" counts as zero
	}
	if len(got) != len(want) {
		t.Fatalf("ParseNumstat returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestCountAuthoredExcludesExempt(t *testing.T) {
	entries := []warn.NumstatEntry{
		{Added: 100, Deleted: 20, Path: "src/feature.ts"},               // 120 authored
		{Added: 40, Deleted: 5, Path: "cmd/floppygit/main.go"},          // 45 authored
		{Added: 900, Deleted: 900, Path: "pnpm-lock.yaml"},              // exempt
		{Added: 500, Deleted: 0, Path: "docs/de/guide.mdx"},             // exempt
		{Added: 300, Deleted: 0, Path: "apps/website/messages/fr.json"}, // exempt
		{Added: 0, Deleted: 0, Path: "logo.png"},                        // binary, zero
	}
	if got := warn.CountAuthored(entries); got != 165 {
		t.Errorf("CountAuthored = %d, want 165 (120+45, exempt excluded)", got)
	}
}

func TestThresholdBoundary(t *testing.T) {
	if warn.Threshold != 150 {
		t.Fatalf("Threshold = %d, want 150", warn.Threshold)
	}
	if got := warn.Warning(150); got != "" {
		t.Errorf("Warning(150) = %q, want empty (150 is at the boundary, no warning)", got)
	}
	if got := warn.Warning(151); got == "" {
		t.Errorf("Warning(151) = empty, want a warning (151 is over the boundary)")
	}
}

func TestWarningCopyCarriesTheRemedy(t *testing.T) {
	got := warn.Warning(212)
	for _, sub := range []string{
		"212",
		"authored lines in this commit",
		"consider splitting",
		"floppygit new",
		"stacked commit for the next unit",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("Warning(212) missing %q\ngot: %q", sub, got)
		}
	}
}

// Commit is a passthrough to `git commit` that prints the size warning
// afterward. A large authored diff must produce the warning.
func TestCommitWarnsOnLargeAuthoredDiff(t *testing.T) {
	r := gittest.NewRepo(t)
	r.WriteFile("big.ts", strings.Repeat("const x = 1;\n", 200))
	r.Add("big.ts")

	var out bytes.Buffer
	err := warn.Commit(gitexec.Git{Dir: r.Dir}, []string{"-m", "feat(x): add a large module with two hundred lines of code"}, &out)
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if r.CountRange("HEAD~1..HEAD") != 1 {
		t.Fatalf("expected exactly one new commit")
	}
	if !strings.Contains(out.String(), "authored lines in this commit") ||
		!strings.Contains(out.String(), "floppygit new") {
		t.Errorf("expected size warning after commit, got: %q", out.String())
	}
}

// Exempt-only diffs (a regenerated lockfile) authored zero lines, so no warning.
func TestCommitSilentWhenOnlyExemptFilesChange(t *testing.T) {
	r := gittest.NewRepo(t)
	r.WriteFile("pnpm-lock.yaml", strings.Repeat("dep: 1.0.0\n", 400))
	r.Add("pnpm-lock.yaml")

	var out bytes.Buffer
	err := warn.Commit(gitexec.Git{Dir: r.Dir}, []string{"-m", "ops(deps): regenerate the pnpm lockfile after dependency bump"}, &out)
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if strings.Contains(out.String(), "authored lines in this commit") {
		t.Errorf("exempt-only change must not warn, got: %q", out.String())
	}
}

// The commit passthrough shells out to real git, so a rejecting commit-msg hook
// fires and the commit does not land: proof floppygit never reimplements git.
func TestCommitHonorsRejectingHook(t *testing.T) {
	r := gittest.NewRepo(t)
	r.InstallHook("commit-msg", "#!/bin/sh\necho 'hook: rejected' >&2\nexit 1\n")
	before := r.Head()
	r.WriteFile("x.txt", "change\n")
	r.Add("x.txt")

	var out bytes.Buffer
	err := warn.Commit(gitexec.Git{Dir: r.Dir}, []string{"-m", "feat(x): this message will be rejected by the commit-msg hook"}, &out)
	if err == nil {
		t.Fatalf("Commit should fail when the commit-msg hook rejects")
	}
	if r.Head() != before {
		t.Errorf("HEAD moved despite hook rejection: %s -> %s", before, r.Head())
	}
	if strings.Contains(out.String(), "authored lines in this commit") {
		t.Errorf("no size warning should print when the commit was rejected, got: %q", out.String())
	}
}
