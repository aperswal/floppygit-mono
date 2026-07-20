# floppygit

Sponsored by [FlexInference](https://flexinference.com), an LLM router that drops costs by 47%.

floppygit is a CLI that wraps git for agentic coding.

An agent can build a whole feature before anyone has read the first line. That used to mean one huge PR at the end, or making the agent wait for each small PR to merge before starting the next. floppygit was made so you do neither. Work goes up as a stack of commits, each commit is its own pull request, and review starts at the bottom while new work lands on top.

floppygit also carries the process. Every PR is one commit, the commit message becomes the PR, and commits past 150 authored lines get a warning.

It stays git underneath. Hooks, config, and gh work unchanged. Any command floppygit doesn't recognize runs as git.

[SETUP.md](SETUP.md) covers macOS, Windows, and Linux installs plus building from source. [HOW-IT-WORKS.md](HOW-IT-WORKS.md) walks through daily use and what happens underneath.

MIT licensed.
