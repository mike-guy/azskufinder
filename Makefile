BINARY    := azskufinder
PKG       := ./cmd/azskufinder
BIN_DIR   := bin
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# The version comes from the latest tag, for example v0.1.0-3-gabc1234-dirty.
# Only the version is injected; Go records the commit and its time in the
# binary itself (see 'go version -m bin/azskufinder'). .goreleaser.yaml sets
# the same ldflags for releases, so keep the two in step.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Pure-Go, statically linked binaries, which also makes cross-compiling trivial.
export CGO_ENABLED := 0

# Development tools run at the versions pinned in mise.toml, whether or not
# your shell has mise activated.
X := mise exec --

FUZZTIME ?= 30s
ARGS     ?=
BASE     ?= origin/main

.DEFAULT_GOAL := help

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

.PHONY: version
version: ## Print the version a build would report
	@echo $(VERSION)

.PHONY: tools
tools: ## Install the development tools pinned in mise.toml
	@command -v mise >/dev/null 2>&1 || { \
		echo "mise is needed: brew install mise (or see https://mise.jdx.dev/getting-started.html)"; exit 1; }
	mise install

.PHONY: hooks
hooks: ## Install the git hooks
	$(X) pre-commit install

##@ Build

.PHONY: build
build: ## Build the binary for this machine into bin/
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

.PHONY: build-check
build-check: ## Compile every package without writing a binary
	go build ./...

.PHONY: build-all
build-all: ## Build for every platform into bin/
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		[ $$os = windows ] && ext=.exe; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(BIN_DIR)/$(BINARY)-$$os-$$arch$$ext $(PKG) || exit 1; \
	done

.PHONY: install
install: ## Install the binary into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

.PHONY: run
run: ## Run the CLI, for example: make run ARGS="vm --help"
	go run -ldflags "$(LDFLAGS)" $(PKG) $(ARGS)

##@ Quality

.PHONY: fmt
fmt: ## Format the code (rewrites files)
	$(X) golangci-lint fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail if any file is not formatted
	$(X) golangci-lint fmt --diff ./...

.PHONY: lint
lint: ## Run the linters
	$(X) golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run the linters and apply their fixes
	$(X) golangci-lint run --fix ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum is not tidy
	go mod tidy -diff

.PHONY: test
test: ## Run every test with the race detector, in random order
	CGO_ENABLED=1 go test -race -shuffle=on -count=1 ./...

.PHONY: test-short
test-short: ## Run the tests with the race detector, using the test cache
	CGO_ENABLED=1 go test -race -short ./...

.PHONY: cover
cover: ## Write coverage.out and print coverage by function
	CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

.PHONY: cover-html
cover-html: cover ## Open the coverage report in a browser
	go tool cover -html=coverage.out

.PHONY: fuzz
fuzz: ## Run each fuzz target for FUZZTIME (default 30s)
	@for pkg in $$(go list ./...); do \
		for target in $$(go test -list '^Fuzz' $$pkg | grep '^Fuzz'); do \
			echo "  $$pkg $$target"; \
			go test -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZTIME) $$pkg || exit 1; \
		done; \
	done

.PHONY: golden-update
golden-update: ## Rewrite the golden files from the current output
	go test ./internal/output ./internal/cli -update

.PHONY: pre-commit
pre-commit: ## Run every pre-commit hook against every file
	$(X) pre-commit run --all-files --show-diff-on-failure

.PHONY: commits-check
commits-check: ## Check each commit since BASE (default origin/main) is a conventional commit
	@msg=$$(mktemp); failed=0; \
	for sha in $$(git rev-list --no-merges "$(BASE)..HEAD"); do \
		git log -1 --format=%B "$$sha" > "$$msg"; \
		if ! $(X) pre-commit run conventional-pre-commit --hook-stage commit-msg \
			--commit-msg-filename "$$msg" > /dev/null; then \
			echo "Not a conventional commit: $$(git log -1 --format='%h %s' "$$sha")"; failed=1; \
		fi; \
	done; \
	rm -f "$$msg"; \
	if [ $$failed -ne 0 ]; then echo "See CONTRIBUTING.md for the format, and how to reword a commit."; exit 1; fi

##@ Security

.PHONY: vuln
vuln: ## Check dependencies and the standard library for known vulnerabilities
	$(X) govulncheck ./...

.PHONY: secrets
secrets: ## Scan the git history and the working tree for secrets
	$(X) gitleaks git --redact --no-banner .
	$(X) gitleaks dir --redact --no-banner .

.PHONY: trivy
trivy: ## Scan for vulnerabilities, secrets and licences (settings in trivy.yaml)
	$(X) trivy fs .

.PHONY: actions-lint
actions-lint: ## Lint the GitHub Actions workflows for errors and security issues
	$(X) actionlint
	$(X) zizmor .github

.PHONY: security
security: vuln secrets trivy actions-lint ## Run every security check

##@ Release (local previews: nothing is tagged or published)

.PHONY: current-version
current-version: ## Print the version of the latest release
	@$(X) svu current

.PHONY: next-version
next-version: ## Print the version the next release would get
	@$(X) svu next

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml
	$(X) goreleaser check

.PHONY: snapshot
snapshot: ## Build every release artifact into dist/ without publishing
	$(X) goreleaser release --snapshot --clean

##@ All

.PHONY: check
check: fmt-check tidy-check build-check lint test vuln secrets actions-lint ## Run every check (do this before pushing)

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN_DIR) dist coverage.out
