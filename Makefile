# cppgo — call C++ from Go with no cgo at build time.
#
# Requirements
#   Go >= 1.25          (go.mod pins the minimum; CI tests stable + 1.25.x)
#   g++ / C++17         only for the example fixtures (`make examples`)
#   golangci-lint >= 2  optional, for `make lint`
#   hugo extended       optional, for `make site`
#
# CGO note — read this before "fixing" the test target
#   cppgo is a *no-cgo* library: everything must build and test with
#   CGO_ENABLED=0, and `make test-nocgo` (pass 1 of `make test`) proves it.
#   The race detector, however, is built on cgo: the toolchain rejects
#   `CGO_ENABLED=0 go test -race` outright with
#       go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
#   So the two properties DESIGN.md asks for cannot be measured by a single
#   command. `make test` runs them as two passes instead of one impossible
#   command: no-cgo first (build/behaviour), then -race with cgo enabled.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
.DELETE_ON_ERROR:
.NOTPARALLEL:

GO              ?= go
CXX             ?= g++
HUGO            ?= hugo
GO_PKGS         ?= ./...
BIN_DIR         ?= bin
SITE_DIR        ?= site
PUBLIC_DIR      ?= site/public
COVER_PROFILE   ?= coverage.out
COVER_THRESHOLD ?= 99.0
COVERMODE       ?= atomic

GREEN  := \033[1;32m
YELLOW := \033[1;33m
RESET  := \033[0m

.PHONY: help build test test-race test-nocgo cover cover-gate bench lint fmt fmt-check \
        examples site clean clean-examples tidy verify

## ---------------------------------------------------------------------------
## Targets
## ---------------------------------------------------------------------------

help: ## Show this help
	@printf 'cppgo — make targets:\n\n'
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_.-]+:.*## / { printf "  \033[1;32m%-14s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf '\nVariables (override on the command line):\n'
	@printf '  %-16s %s\n' GO "$(GO)" CXX "$(CXX)" HUGO "$(HUGO)"
	@printf '  %-16s %s\n' GO_PKGS "$(GO_PKGS)"
	@printf '  %-16s %s\n' COVER_PROFILE "$(COVER_PROFILE)"
	@printf '  %-16s %s (gate fails below this)\n' COVER_THRESHOLD "$(COVER_THRESHOLD)"
	@printf '\nExamples:\n'
	@printf '  make test COVER_THRESHOLD=98   # one-off gate loosened for a spike\n'
	@printf '  make cover-gate               # regenerates $(COVER_PROFILE) if absent\n'

build: ## Compile every package with CGO_ENABLED=0 (the no-cgo contract), then cmd/ binaries
	CGO_ENABLED=0 $(GO) build $(GO_PKGS)
	@if [ -d cmd ]; then \
		for dir in cmd/*/; do \
			[ -d "$$dir" ] || continue; \
			compgen -G "$$dir/*.go" >/dev/null || { \
				printf '  $(YELLOW)skip$(RESET)  %s (no Go files yet)\n' "$$dir"; \
				continue; \
			}; \
			name=$$(basename "$$dir"); \
			printf '  $(GREEN)build$(RESET) %s -> $(BIN_DIR)/%s\n' "$$name" "$$name"; \
			mkdir -p $(BIN_DIR); \
			CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/$$name ./$$dir; \
		done; \
	fi

test: ## Two passes: CGO_ENABLED=0 (no-cgo contract) then -race (needs cgo)
	@printf '== pass 1/2: CGO_ENABLED=0 go test -count=1 %s ==\n' "$(GO_PKGS)"
	CGO_ENABLED=0 $(GO) test -count=1 $(GO_PKGS)
	@printf '== pass 2/2: CGO_ENABLED=1 go test -race -count=1 %s ==\n' "$(GO_PKGS)"
	CGO_ENABLED=1 $(GO) test -race -count=1 $(GO_PKGS)

test-race: ## Race detector only (requires cgo; GOMAXPROCS=1 run is in the tests)
	CGO_ENABLED=1 $(GO) test -race -count=1 $(GO_PKGS)

test-nocgo: ## Test with cgo disabled — proves the package needs no cgo
	CGO_ENABLED=0 $(GO) test -count=1 $(GO_PKGS)

cover: ## Run tests with atomic coverage and write the coverage profile
	@rm -f $(COVER_PROFILE)
	CGO_ENABLED=1 $(GO) test -race -count=1 -covermode=$(COVERMODE) -coverprofile=$(COVER_PROFILE) $(GO_PKGS)
	@if [ -s $(COVER_PROFILE) ]; then \
		printf '== total (go tool cover -func) ==\n'; \
		$(GO) tool cover -func=$(COVER_PROFILE) | tail -n 1; \
	else \
		printf '!! $(COVER_PROFILE) was not written (no packages instrumented?)\n'; \
		exit 1; \
	fi

cover-gate: ## Enforce the statement-coverage threshold (regenerates the profile if missing)
	@if [ ! -s $(COVER_PROFILE) ]; then \
		printf '>> $(COVER_PROFILE) not found — running "make cover" first\n'; \
		if ! $(MAKE) --no-print-directory cover; then \
			printf '$(YELLOW)!! could not produce $(COVER_PROFILE) — nothing to gate.$(RESET)\n'; \
			exit 2; \
		fi; \
	fi
	@./scripts/coverage-gate.sh $(COVER_PROFILE) $(COVER_THRESHOLD)

bench: ## Run all benchmarks (-benchmem); results are the ones quoted in the docs
	CGO_ENABLED=0 $(GO) test -run '^$$' -bench . -benchmem $(GO_PKGS)

lint: ## go vet, then golangci-lint when installed
	CGO_ENABLED=0 $(GO) vet $(GO_PKGS)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout=5m $(GO_PKGS); \
	else \
		printf '$(YELLOW)!! golangci-lint not found$(RESET) — go vet was the only check.\n'; \
		printf '   install: https://golangci-lint.run/docs/welcome/install/\n'; \
	fi

fmt: ## Format Go and shell sources in place
	$(GO) fmt $(GO_PKGS)
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -local github.com/Manu343726/cppgo -w $$($(GO) list -f '{{.Dir}}' $(GO_PKGS) 2>/dev/null || true) 2>/dev/null || true; \
	fi
	@if command -v shfmt >/dev/null 2>&1; then shfmt -w -i 2 -s scripts/; fi

fmt-check: ## Fail if any Go file is not gofmt-clean
	@unformatted=$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*' 2>/dev/null)); \
	if [ -n "$$unformatted" ]; then \
		printf '$(YELLOW)!! gofmt needed:$(RESET)\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

examples: ## Build the C++ fixtures in examples/ (see scripts/build-examples.sh)
	@./scripts/build-examples.sh

site: ## Build the Hugo site from site/ into site/public/
	@if [ ! -f $(SITE_DIR)/hugo.toml ]; then \
		printf '$(YELLOW)!! $(SITE_DIR)/hugo.toml is missing$(RESET) — no site to build yet.\n'; \
		exit 1; \
	fi
	@if ! command -v $(HUGO) >/dev/null 2>&1; then \
		printf '$(YELLOW)!! hugo not found$(RESET) — install the *extended* build (CI pins 0.169.0).\n'; \
		exit 1; \
	fi
	$(HUGO) version
	$(HUGO) --source $(SITE_DIR) --gc --minify --destination "$$(pwd)/$(PUBLIC_DIR)"

clean-examples: ## Remove build outputs left inside examples/
	@if [ -d examples ]; then \
		find examples -mindepth 2 -maxdepth 2 -type d -name build -prune -exec rm -rf {} + 2>/dev/null || true; \
		find examples -mindepth 2 -maxdepth 2 -type f -name 'lib*.so' -delete 2>/dev/null || true; \
	fi

clean: ## Remove build, coverage and site output
	rm -rf $(BIN_DIR) $(PUBLIC_DIR) $(SITE_DIR)/resources $(SITE_DIR)/.hugo_build.lock $(COVER_PROFILE)
	@$(MAKE) --no-print-directory clean-examples

tidy: ## go mod tidy + go mod verify
	$(GO) mod tidy
	$(GO) mod verify

verify: build lint test cover-gate examples ## Everything CI runs, in one shot
