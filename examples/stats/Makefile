# stats — a large []float64 crossing the boundary, with a benchmark.
#
#   make              build libstats.so, generate the bindings, test and vet
#   make bench        benchmark the C++ call against the pure-Go equivalent
#   make check        prove the committed generated files are up to date
#   make clean
#
# Everything is built under build/, which is git-ignored: the shared library, the
# cppgo binary (built from the repository this example lives in, so there is no
# install step and no chance of testing a different cppgo than the one next door)
# and the compilation database.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
CXX      ?= g++
CXXFLAGS ?= -std=c++17 -O2 -fPIC -Wall -Wextra

BUILD := build
ROOT  := ../..
LIB   := $(BUILD)/libstats.so
CPPGO := $(BUILD)/cppgo
CDB   := $(BUILD)/compile_commands.json

SOURCES  := src/stats.cpp
HEADERS  := include/stats/stats.hpp
INCLUDES := -Iinclude -Iinclude/stats

# The generator is given the same compilation database the compiler gets, so the
# header is parsed with this project's real flags and the report says
# "abi from compile_commands.json" instead of warning that the flags are a guess.
CPPGO_ARGS := -dir . -compile-commands $(CURDIR)/$(CDB)
TEST_ENV   := CPPGO_STATS_LIB=$(CURDIR)/$(LIB)

.PHONY: all lib gen test vet check bench clean

all: test vet

## --- the C++ side ----------------------------------------------------------

lib: $(LIB)

$(LIB): $(SOURCES) $(HEADERS) | $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -o $(LIB) $(SOURCES)

# What a CMake build writes with CMAKE_EXPORT_COMPILE_COMMANDS=ON. Written by
# hand only because this example does not need a whole build system.
$(CDB): $(SOURCES) $(HEADERS) | $(BUILD)
	@printf '[{"directory": "%s", "arguments": ["%s", "-std=c++17", "-fPIC", "-I%s/include", "-I%s/include/stats", "-c", "%s/src/stats.cpp"], "file": "%s/src/stats.cpp"}]\n' \
		"$(CURDIR)" "$(CXX)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > $(CDB)

## --- the Go side -----------------------------------------------------------

gen: $(CPPGO) $(CDB)
	$(CPPGO) gen $(CPPGO_ARGS)

test: lib gen
	$(TEST_ENV) $(GO) test -count=1 -v ./...

vet: gen
	$(GO) vet ./...

check: $(CPPGO) $(CDB)
	$(CPPGO) check $(CPPGO_ARGS)

bench: lib gen
	$(TEST_ENV) $(GO) test -run '^$$' -bench . -benchmem ./...

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

clean:
	rm -rf $(BUILD)
