package warn

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
)

// Threshold is the authored-line count above which a commit draws the split
// heads-up. It never blocks; it only prints.
const Threshold = 150

// exemptRe mirrors the EXEMPT regex in scripts/check-commit-msg.sh of the
// flexinference-mono repo: lockfiles anywhere, translated docs and the faq
// source, generated SDK/contract/pricing artifacts, and generated message
// catalogs with their hash manifest. Hand-authored source and copy - including
// en.json and untranslated docs - are NOT here and so are counted. This is the
// single source of the exempt set in floppygit; keep it in sync with that
// script, which is where the categories are defined.
var exemptRe = regexp.MustCompile(`pnpm-lock\.yaml$|package-lock\.json$|uv\.lock$|^docs/faq\.mdx$|^docs/(de|es|fr|hi|pt|zh)/.*\.mdx$|^harness/matrix\..*\.json$|^packages/sdk-typescript/src/types\.ts$|^packages/sdk-python/src/flexinference/models\.py$|^contract/flexinference-openapi\.yaml$|^apps/website/messages/(de|es|fr|hi|pt|zh)\.json$|^apps/website/messages/\.hashes\.json$|^packages/core/src/pricing-data\.generated\.ts$`)

// IsExempt reports whether a path is a generated or translated artifact that no
// human hand-authored, and so is excluded from the authored-line count.
func IsExempt(path string) bool {
	return exemptRe.MatchString(path)
}

// NumstatEntry is one file's line delta from `git diff --numstat`.
type NumstatEntry struct {
	Added   int
	Deleted int
	Path    string
}

// ParseNumstat parses `git diff --numstat` output. A binary file shows "-" for
// its counts, which parse to zero.
func ParseNumstat(raw string) []NumstatEntry {
	var entries []NumstatEntry
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		entries = append(entries, NumstatEntry{
			Added:   parseCount(fields[0]),
			Deleted: parseCount(fields[1]),
			Path:    fields[2],
		})
	}
	return entries
}

func parseCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "-" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// CountAuthored sums added plus deleted lines across the non-exempt files.
func CountAuthored(entries []NumstatEntry) int {
	total := 0
	for _, e := range entries {
		if IsExempt(e.Path) {
			continue
		}
		total += e.Added + e.Deleted
	}
	return total
}

// Warning returns the split heads-up for n authored lines, or "" at or under the
// threshold. The wording is authoritative in README.md's daily-use section.
func Warning(n int) string {
	if n <= Threshold {
		return ""
	}
	return fmt.Sprintf(
		"%d authored lines in this commit; consider splitting -\n"+
			"floppygit new <name> starts a stacked commit for the next unit.", n)
}

// Commit is `floppygit commit`: it passes its arguments straight to `git commit`
// (so the commit-msg hook, editor, and template all fire), then, if the commit
// landed, prints the size heads-up for HEAD's authored line count. It returns
// git's error, so a rejected commit blocks and nothing is printed.
func Commit(g gitexec.Git, args []string, out io.Writer) error {
	if err := g.Run(append([]string{"commit"}, args...)...); err != nil {
		return err
	}
	raw, err := g.Output("diff-tree", "--no-commit-id", "--numstat", "-r", "--root", "HEAD")
	if err != nil {
		// The commit landed; we just could not measure it. Do not fail on that.
		return nil
	}
	if w := Warning(CountAuthored(ParseNumstat(raw))); w != "" {
		fmt.Fprintln(out, w)
	}
	return nil
}
