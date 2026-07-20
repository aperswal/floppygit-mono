# How it works

## Daily use

Start a stack and commit the first piece:

```sh
floppygit new metering-table
floppygit commit -m "feat(billing): add usage metering table and writer"
```

`new` branches off your current commit and records the parent, so floppygit can walk the chain later. `commit` is git commit plus the size check. Past 150 authored lines it warns and moves on:

```
212 authored lines in this commit; consider splitting -
floppygit new <name> starts a stacked commit for the next unit.
```

Lockfiles, generated code, and translated files don't count toward that number.

Stack the next piece on top and push:

```sh
floppygit new metering-ui
floppygit commit -m "feat(billing): usage meter component on the dashboard"
floppygit push
```

`push` opens a PR per branch, bottom first. Each PR targets the branch below it and the bottom one targets main. Auto-merge arms on the bottom alone, so the stack merges in order. On a branch with no stack recorded, push opens a single PR against main.

After the bottom PR merges:

```sh
floppygit sync
```

`sync` rebases the stack onto the latest main and drops the merged commit. It deletes the merged branch and arms auto-merge on the new bottom. A conflict stops the run and prints the commands to finish it.

## When a reviewer asks for a change

Edit the files, then point `fix` at the branch or commit that owns the change:

```sh
floppygit fix metering-table
```

`fix` folds your edit into that commit with an autosquash rebase. It moves every branch above it and force-pushes the stack. The PR shows the corrected commit with no fixup commit left behind.

## Everything else

Any command floppygit doesn't own runs as git:

```sh
floppygit status
floppygit log --oneline
```

## Underneath

Each branch records its parent in git config, under `branch.<name>.floppyParent`. That's the whole model. A chain of one-commit branches, and no sidecar file to drift out of sync with your repo.

`fix` and `sync` run one git rebase with autosquash and update-refs. Every branch above the change moves in the same pass, so the stack is never left half-updated.

The merged bottom commit is patch-identical to your local one, so the next `sync` rebase drops it and the PR above moves to the bottom.

Everything shells out to the git and gh on your PATH. No embedded git, no daemon, and no state outside your repo.
