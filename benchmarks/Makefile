# bench — the cppgo vs cgo vs plain-Go comparison suite.
#
#   make              build the C++ library, generate the cppgo package, test
#                     and vet every arm
#   make lib          build build/libbench.so with the library's own command
#   make gen          run cppgo gen: the generated Go API, assembly, shim
#                     sources, and the compiled shim libcppgo_benchcpp.so
#   make check        prove the committed generated sources are current
#   make test         go test for every arm: CGO_ENABLED=0 for gobase and
#                     cppgo, CGO_ENABLED=1 for cgo
#   make bench        run every arm with -benchmem -count=N and regenerate the
#                     table in README.md from the run
#   make table        regenerate README.md from results/ without re-running
#   make vet          go vet for both cgo modes
#   make clean
#
# CGO: the cgo package is the only part of this module that needs cgo. The
# cppgo package and the plain-Go baseline are built and run with
# CGO_ENABLED=0, which is the property the project promises; `make test` and
# `make vet` run each package under the mode it needs instead of one mode for
# the whole module.
#
# The generated *sources* are committed, like the examples': the suite is
# teaching material for the comparison and `make check` proves the committed
# files are what the generator writes today. Only the compiled objects
# (build/, libcppgo_benchcpp.so) are ignored.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO       ?= go
CXX      ?= g++
CXXFLAGS ?= -std=c++17 -O2 -fPIC -Wall -Wextra

BUILD := build
LIB   := $(BUILD)/libbench.so
CPPGO := $(BUILD)/cppgo
SHIM  := cppgo/libcppgo_benchcpp.so
ROOT  := ..

HEADER := include/bench/bench.hpp
SOURCE := src/bench.cpp
INCLUDES := -Iinclude

# The exact generation command, paths relative to this directory, so the banner
# in the generated file is one a reader can copy. -status is what turns
# bench::parse's integer into a Go error.
GEN_ARGS := -header $(HEADER) -package benchcpp -library $(LIB) -out cppgo -status bench::parse

# The cgo package is the only one that cannot build with cgo off, so the two
# passes name their packages instead of using ./...
NO_CGO_PKGS := ./gobase ./cppgo ./internal/... ./cmd/...
CGO_PKGS    := ./cgo

BENCH_COUNT ?= 6

GREEN  := \033[1;32m
YELLOW := \033[1;33m
RESET  := \033[0m

.PHONY: help all lib gen check test bench table vet fmt fmt-check clean

help: ## Show this help
	@printf 'bench — targets:\n\n'
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_.-]+:.*## / { printf "  \033[1;32m%-10s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf '\nVariables:\n'
	@printf '  %-12s %s\n' BENCH_COUNT "$(BENCH_COUNT) (-count for each arm)"

all: gen test vet ## gen, test and vet everything

## --- the C++ library -------------------------------------------------------

lib: $(LIB) ## Build build/libbench.so with the library's own flags

$(LIB): $(SOURCE) $(HEADER) | $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -Wl,-soname,libbench.so -o $@ $(SOURCE)

## --- the generated binding -------------------------------------------------

gen: $(CPPGO) $(LIB) ## Generate and compile the cppgo package
	$(CPPGO) gen $(GEN_ARGS)

check: $(CPPGO) $(LIB) ## Prove the committed generated sources are current
	$(CPPGO) check $(GEN_ARGS)

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

## --- tests, vet, benchmarks ------------------------------------------------

test: gen ## Test every arm in the cgo mode it needs
	@printf '== pass 1/2: CGO_ENABLED=0 (gobase, cppgo) ==\n'
	CGO_ENABLED=0 $(GO) test -count=1 $(NO_CGO_PKGS)
	@printf '== pass 2/2: CGO_ENABLED=1 (cgo) ==\n'
	CGO_ENABLED=1 $(GO) test -count=1 $(CGO_PKGS)

vet: gen ## go vet in both cgo modes
	CGO_ENABLED=0 $(GO) vet $(NO_CGO_PKGS)
	CGO_ENABLED=1 $(GO) vet ./...

bench: gen ## Run the whole matrix and regenerate the table in README.md
	BENCH_COUNT=$(BENCH_COUNT) ./scripts/run.sh

table: ## Regenerate README.md from the committed results, without re-running
	CGO_ENABLED=0 $(GO) run ./cmd/benchtable -write README.md -env results/environment.txt \
		gobase=results/gobase.txt cppgo=results/cppgo.txt cgo=results/cgo.txt

fmt: ## gofmt the Go sources in place
	$(GO) fmt $(NO_CGO_PKGS)

fmt-check: ## Fail if a Go file is not gofmt-clean
	@unformatted=$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*')); \
	if [ -n "$$unformatted" ]; then \
		printf '$(YELLOW)!! gofmt needed:$(RESET)\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

clean: ## Remove build output
	rm -rf $(BUILD) $(SHIM)
