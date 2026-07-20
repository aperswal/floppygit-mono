// Command floppygit is a daily git wrapper that makes a stack of small commits
// the default shape for large work. Each commit becomes its own one-commit pull
// request; the PRs chain bottom-to-top and merge bottom-first.
//
// It is a wrapper, never a git reimplementation: every operation shells out to
// the system git binary, so repository hooks (commit-msg, pre-push) and local
// config fire exactly as under plain git. Any subcommand floppygit does not own
// is exec'd straight through to git so it can be a daily driver.
//
//	new <name>         start a stacked branch off the current tip
//	push               push the chain, one PR per level, auto-merge the bottom
//	                   (a branch not started with `new` falls back to a single PR)
//	fix <sha|branch>   fixup+autosquash a commit and restack everything above it
//	sync               restack the chain onto origin/main
//	commit [args...]   git commit, then print the 150-line size warning
//	<anything else>    passthrough to system git (floppygit status -> git status)
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/aperswal/floppygit-mono/internal/gitexec"
	"github.com/aperswal/floppygit-mono/internal/parity"
	"github.com/aperswal/floppygit-mono/internal/stack"
	"github.com/aperswal/floppygit-mono/internal/warn"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	g := gitexec.Git{}

	// No args behaves like plain git: print git's own usage.
	if len(args) == 0 {
		return gitexec.Passthrough(g, args)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return fail(gitexec.EnvFault("floppygit: git is not on PATH; install git first"))
	}

	sub, rest := args[0], args[1:]

	switch sub {
	case "new":
		if len(rest) < 1 {
			return fail(gitexec.UserFault("usage: floppygit new <name>"))
		}
		return fail(stack.New(stackDeps(g, os.Stdout), rest[0]))

	case "commit":
		return fail(warn.Commit(g, rest, os.Stdout))

	case "push":
		if code := requireGH(); code != 0 {
			return code
		}
		if isStacked(g) {
			return fail(stack.Push(stackDeps(g, os.Stdout)))
		}
		return fail(parity.Run(parityDeps(g, os.Stdout)))

	case "fix":
		if len(rest) < 1 {
			return fail(gitexec.UserFault("usage: floppygit fix <sha-or-branch>"))
		}
		return fail(stack.Fix(stackDeps(g, os.Stdout), rest[0]))

	case "sync":
		if code := requireGH(); code != 0 {
			return code
		}
		return fail(stack.Sync(stackDeps(g, os.Stdout)))

	default:
		return gitexec.Passthrough(g, args)
	}
}

func stackDeps(g gitexec.Git, out io.Writer) stack.Deps {
	return stack.Deps{Git: g, GH: gitexec.NewGH(""), Out: out}
}

func parityDeps(g gitexec.Git, out io.Writer) parity.Deps {
	return parity.Deps{Git: g, GH: gitexec.NewGH(""), Out: out}
}

// requireGH returns 0 when gh is available, or a printed environment-fault exit
// code when it is not, for the commands that open or merge pull requests.
func requireGH() int {
	if _, err := exec.LookPath("gh"); err != nil {
		return fail(gitexec.EnvFault("floppygit: the GitHub CLI (gh) is not on PATH; install it and run gh auth login"))
	}
	return 0
}

// isStacked reports whether the current branch was started with floppygit new
// (it has a recorded floppyParent), which decides whether push walks a stack or
// falls back to the single-PR path.
func isStacked(g gitexec.Git) bool {
	branch, _ := g.Output("branch", "--show-current")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false
	}
	v, _ := g.Output("config", "--local", "--get", "branch."+branch+".floppyParent")
	return strings.TrimSpace(v) != ""
}

func fail(err error) int {
	if err == nil {
		return gitexec.ExitOK
	}
	fmt.Fprintln(os.Stderr, err.Error())
	return gitexec.CodeOf(err)
}
