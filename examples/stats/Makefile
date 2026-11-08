# stats — a large []float64 in both directions, with a benchmark.
#
#   make        build the user's library, generate the binding, test and vet
#   make lib    build build/libstats.so with the project's own command
#   make gen    run cppgo gen: the Go API, the assembly, the shim sources and
#               the shim libcppgo_stats.so
#   make test   go test, once with CGO_ENABLED=0 and once with the race detector
#   make bench  the C++ calls against the pure-Go equivalents
#   make check  prove the committed generated sources are up to date
#   make clean
#
# Everything is built under build/, which is git-ignored: the user's shared
# object and the locally built cppgo. The shim binary cppgo writes beside the
# package (libcppgo_stats.so) is ignored too. The generated *sources* are
# committed, because the example is teaching material: `make gen` reproduces
# them and `make check` proves they are current.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
CXX      ?= g++
CXXFLAGS ?= -std=c++17 -O2 -fPIC -Wall -Wextra

BUILD  := build
ROOT   := ../..
LIB    := $(BUILD)/libstats.so
CPPGO  := $(BUILD)/cppgo
SHIM   := libcppgo_stats.so

HEADER   := include/stats/stats.hpp
SOURCE   := src/stats.cpp
INCLUDES := -Iinclude

# The exact generation command, with paths relative to this directory, so the
# command in the generated file's banner is one a reader can copy. The .clangd
# beside this Makefile is the project's compilation configuration.
GEN_ARGS := -header $(HEADER) -package stats -library $(LIB) -out .

TEST_ENV := CPPGO_STATS_LIB=$(CURDIR)/$(LIB) CPPGO_STATS_SHIM=$(CURDIR)/$(SHIM)

.PHONY: all lib gen test vet check bench clean

all: gen test vet

## --- the user's library ----------------------------------------------------

lib: $(LIB)

$(LIB): $(SOURCE) $(HEADER) | $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -Wl,-soname,libstats.so -o $@ $(SOURCE)

## --- the generated binding -------------------------------------------------

gen: $(CPPGO) $(LIB)
	$(CPPGO) gen $(GEN_ARGS)

test: gen
	CGO_ENABLED=0 $(TEST_ENV) $(GO) test -count=1 -v .
	CGO_ENABLED=1 $(TEST_ENV) $(GO) test -race -count=1 .

vet: gen
	CGO_ENABLED=0 $(GO) vet .

check: $(CPPGO) $(LIB)
	$(CPPGO) check $(GEN_ARGS)

bench: gen
	CGO_ENABLED=0 $(TEST_ENV) $(GO) test -run '^$$' -bench . -benchmem .

## --- tools and housekeeping ------------------------------------------------

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

clean:
	rm -rf $(BUILD) $(SHIM)
