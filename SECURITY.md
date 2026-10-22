# Security policy

## Supported versions

Only the latest release gets security fixes. Any fixes come
out as a new patch, minor or major release.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through
[GitHub private vulnerability reporting](https://github.com/mike-guy/azskufinder/security/advisories/new).

Include what you found, how to reproduce it, and the output of
`azskufinder --version`. You should get a reply within a week. Once a fix is
released, the advisory is published, with credit to you unless you'd rather not
be named.

## Verifying a release

Every release archive is built by GitHub Actions from a tagged commit, and comes
with a signed [build provenance attestation](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations).
It proves which workflow, repository and commit produced the file. To check a
download with the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify azskufinder_0.1.0_darwin_arm64.tar.gz --repo mike-guy/azskufinder
```

The attestation is also attached to each release as
`azskufinder_<version>.sigstore.json`. Add `--bundle` with that file to check a
download offline.

Each release also has a `checksums.txt` file:

```sh
sha256sum --check --ignore-missing checksums.txt
```

and an SPDX software bill of materials (`*.sbom.json`) for each binary, listing
every dependency compiled into it.

## How the project is checked

Every pull request is checked by
[govulncheck](https://go.dev/doc/security/vuln/), [CodeQL](https://codeql.github.com/),
[gosec](https://github.com/securego/gosec) (through golangci-lint),
[Trivy](https://trivy.dev/) (vulnerabilities, secrets and licences),
[gitleaks](https://github.com/gitleaks/gitleaks) and GitHub dependency review.
The workflows themselves are checked by [zizmor](https://docs.zizmor.sh/). The
vulnerability scans also run every week against `main`.

Every dependency and tool is pinned: Go modules by `go.sum`, development tools
by checksum in `mise.lock`, and GitHub Actions by commit SHA.
[Renovate](https://docs.renovatebot.com/) proposes updates every week, and
raises security fixes straight away.
