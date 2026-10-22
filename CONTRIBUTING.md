# Contributing to azskufinder

Thanks for helping. This guide covers setting up, the checks your change must
pass, and how commit messages turn into releases. How the code is structured and
tested is described in [ARCHITECTURE.md](ARCHITECTURE.md).

## Setting up

You need [Go](https://go.dev/dl/), `make` and [mise](https://mise.jdx.dev/):

```sh
brew install mise   # or see https://mise.jdx.dev/getting-started.html
mise trust          # allow this repository's mise.toml
make tools          # installs the development tools
make hooks          # installs the git hooks
```

Every development tool (golangci-lint, govulncheck, svu, actionlint, zizmor,
gitleaks, Trivy, GoReleaser and pre-commit) is pinned in `mise.toml`.
`mise.lock` records each download's checksum, so you, CI and the release all
use exactly the same versions. The Makefile runs them through `mise exec`, so
they work whether or not you've added mise to your shell. Run
`mise activate` (see mise's docs) if you'd also like them on your `PATH`.

Go isn't managed by mise. The `toolchain` line in `go.mod` sets the version,
and Go downloads it for you if yours is older.

## Everyday commands

`make` on its own lists every target. The ones you'll use most are:

| Command | What it does |
|---|---|
| `make build` | Builds `bin/azskufinder` for your machine |
| `make run ARGS="vm --help"` | Runs the CLI from source |
| `make test` | Runs every test with the race detector |
| `make lint` / `make fmt` | Lints the code / formats it |
| `make golden-update` | Rewrites golden files after an intended output change |
| `make check` | Runs everything CI runs. Do this before you push |
| `make commits-check` | Checks your branch's commit messages (against `origin/main`, or `BASE=<ref>`) |
| `make pre-commit` | Runs every git hook against every file |
| `make snapshot` | Builds every release artifact into `dist/`, without publishing anything |

## Git hooks

`pre-commit install` adds hooks that run when you commit and push. Each runs
only when files it cares about are staged:

- **on commit:** formatting, `go mod tidy`, build, lint, tests, workflow linting,
  secret scanning (gitleaks, on what you're committing) and whitespace fixes.
  They also check that your commit message is a conventional commit.
- **on push:** govulncheck, which needs the network

If a hook reformats a file, the commit stops so you can review the change and
stage it again. To skip a slow hook for a work-in-progress commit, name it:

```sh
SKIP=go-test,go-lint git commit -m "wip: try a different approach"
```

CI runs every check anyway, so nothing skipped locally gets past a pull request.

## Commit messages and releases

Every commit must be a [conventional commit](https://www.conventionalcommits.org/).
Merging into `main` doesn't release anything. The maintainer releases when
`main` is ready, and the commit types since the last release decide the new
version:

| Commit | Example | Release |
|---|---|---|
| `feat:` | `feat(vm): add --zones filter` | minor: 0.3.0 → 0.4.0 |
| `fix:` | `fix: handle an empty price list` | patch: 0.3.0 → 0.3.1 |
| `!` or a `BREAKING CHANGE:` footer | `feat!: rename --cpus to --vcpus` | minor while on v0.x, major from v1 |
| `perf:` `docs:` `test:` `refactor:` `build:` `ci:` `chore:` `revert:` | `docs: add a jq example` | no release on its own |

The release notes list `feat:`, `fix:` and `perf:` commits, and breaking changes
under a heading of their own. Only a `!` in the subject puts a commit under that
heading, so use one for every breaking change.

Use the type that describes the change for a user: a refactor that fixes a bug
is a `fix:`. `make next-version` shows the version your branch would release.

To release, run the release workflow on `main`:

```sh
gh workflow run release.yml                     # works out the version from the commits
gh workflow run release.yml -f version=v1.0.0   # or choose it, for example for v1.0.0
gh workflow run release.yml -f dry_run=true     # build everything, publish nothing
```

It tags the release, publishes it with GoReleaser, and updates the Homebrew tap
and Scoop bucket. If no commit since the last release calls for a new version,
it stops without releasing. If it fails part way through, re-running the
failed jobs is safe.

A message that isn't conventional fails the `commits` check on the pull request.
To fix the most recent commit, run `git commit --amend`. For an older one, run
`git rebase -i main`, mark the commit `reword`, then `git push --force-with-lease`.

## Dependency updates

[Renovate](https://docs.renovatebot.com/) checks every Monday and opens pull
requests for anything out of date. It covers Go modules and the Go toolchain
(`go.mod`), development tools and mise itself (`mise.toml`, `mise.lock` and the
workflows), GitHub Actions, and pre-commit hooks. Its settings are in
`.github/renovate.json5`, and the **Dependency Dashboard** issue lists everything
it's tracking or waiting on.

- Minor and patch updates are grouped, one pull request per kind of
  dependency. Each major update gets a pull request of its own.
- Go module and toolchain updates are `fix(deps):` commits, so the next
  release is at least a patch: the new code ships in the binary. Everything
  else is `chore(deps):` and doesn't call for a release.
- Security fixes are raised straight away, labelled `security`.
- Nothing merges automatically. Review each pull request, and merge it once the
  checks pass.

To add or upgrade a tool yourself, run `mise use <tool>@<version>`, which
updates `mise.toml` and `mise.lock`, and commit both files. Never pin a version
inside a workflow `run:` step or a script, where Renovate can't see it.

The `go` line in `go.mod` is the oldest Go that can build azskufinder, and
it matters to anyone who runs `go install`. Raise it only when the code needs
something newer. Renovate updates the `toolchain` line, which is the Go that CI
and releases are built with, so releases pick up Go's security fixes.

## Pull requests

1. Branch from `main`, and keep each pull request to one change.
2. Add or update tests for any change in behaviour, and update `README.md` if a
   flag, default or output has changed.
3. Run `make check`.
4. Open the pull request. Every check must pass, and the branch must be up to
   date with `main`. Use **Update branch** if GitHub asks.
5. Merge with a merge commit. Each commit's subject appears in the next
   release's notes.

## Reporting security issues

Please don't open an issue. See [SECURITY.md](SECURITY.md).
