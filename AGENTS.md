# Agent instructions

azskufinder is a Go CLI that finds the Azure VM sizes a subscription can deploy.

- **Read [ARCHITECTURE.md](ARCHITECTURE.md) before changing Go code.** It covers
  the layout, the import rules between packages, how dependencies are wired, and
  how to test. For a new flag, filter, sort column, resource command or input
  check, follow its Recipes section.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** has the `make` targets and tooling. Use
  `make test` and `make lint` while working, and run `make check` before saying
  a change is done. If anything fails, quote the output; don't describe it as
  passing.
- **Never run git commands** that change the repository or a remote: no `git
  add`, `commit`, `push`, `rebase`, `reset`, `stash` or `checkout`. The
  maintainer does all of that. Reading with `git status`, `git diff` or `git
  log` is fine. When a change is ready, say so, and suggest a commit message if
  it helps.
- **Never use `-update` to get a failing `TestVMDocument_schema` to pass.** A
  removed, renamed or retyped JSON field needs a new `jsonAPIVersion`. After
  `make golden-update`, read the golden file diff and say what changed.
- **Keep the docs in step.** When a flag, default or output changes, update
  `README.md`. The `--sort-by` column list is written out by hand in both the
  README and the `vm` command's `Long` help text in `internal/cli/vm.go`.
- **Write in British English** (colour, behaviour), in code comments and docs
  alike. The misspell linter checks this.
