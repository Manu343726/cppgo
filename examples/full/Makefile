# full — the complete example: a user's C++ library with its own CMake build,
# a cppgo-generated Go binding, a real Go program and its tests.
#
#   make          build the C++ with CMake, generate the binding, build the app
#   make lib      the user's library only: build/libmetrics.so, by CMake
#   make gen      cppgo gen: the generic Go API, the assembly, the shim sources
#                 and the shim libcppgo_metrics.so
#   make scan     cppgo build -scan ./cmd/...: the template instantiations the
#                 program uses are added to the shim and it is rebuilt
#   make app      go build ./cmd/app into build/app
#   make test     go test, once with CGO_ENABLED=0 and once with the race detector
#   make run      build and start the program
#   make check    prove the committed generated sources are up to date
#   make clean
#
# Everything is built under build/, which is git-ignored: the CMake tree, the
# user's shared object, the locally built cppgo and the Go binary. The generated
# *sources* under gen/ are committed, because the example is teaching material:
# `make gen` and `make scan` reproduce them and `make check` proves they are
# current.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO    ?= go
CMAKE ?= cmake

ROOT  := ../..
BUILD := build
GEN   := gen
CPPGO := $(BUILD)/cppgo
LIB   := $(BUILD)/libmetrics.so
APP   := $(BUILD)/app
SHIM  := $(GEN)/libcppgo_metrics.so

HEADER := include/metrics/metrics.hpp
SOURCE := src/metrics.cpp

# The exact generation command, with paths relative to this directory, so the
# command in the generated file's banner is one a reader can copy. The flags
# for the header come from the CMake compilation database, which is what makes
# the shim compile with the flags the library was built with.
GEN_ARGS := -header $(HEADER) -package metrics -library $(LIB) \
            -compile-commands $(BUILD)/compile_commands.json -out ./$(GEN) \
            -status metrics::validate_name

# Phase two of the template flow: collect the instantiations the program uses.
SCAN_ARGS := -scan ./cmd/... -scan-dir .

.PHONY: all lib configure gen scan app test vet run check clean

all: app test vet

## --- the user's library, built by CMake ------------------------------------

lib: $(LIB)

configure: $(BUILD)/CMakeCache.txt

# CMAKE_EXPORT_COMPILE_COMMANDS is set in CMakeLists.txt, so the database
# cppgo reads is a product of the project's own build.
$(BUILD)/CMakeCache.txt: CMakeLists.txt | $(BUILD)
	$(CMAKE) -S . -B $(BUILD)

$(LIB): $(BUILD)/CMakeCache.txt $(SOURCE) $(HEADER)
	$(CMAKE) --build $(BUILD)

## --- the generated binding -------------------------------------------------

# Phase one writes the generic API: it compiles with no concrete template
# argument, which is what lets the Go program be written before the shim knows
# which specializations it uses.
gen: $(CPPGO) $(LIB)
	$(CPPGO) gen $(GEN_ARGS)

# Phase two collects the instantiations from the program and adds them to the
# shim before rebuilding it.
scan: gen
	$(CPPGO) build $(GEN_ARGS) $(SCAN_ARGS)

## --- the program and its tests ---------------------------------------------

app: scan
	CGO_ENABLED=0 $(GO) build -o $(APP) ./cmd/app

test: scan
	CGO_ENABLED=0 $(GO) test -count=1 -v ./...
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

vet: scan
	CGO_ENABLED=0 $(GO) vet ./...

run: app
	./$(APP)

check: $(CPPGO) $(LIB)
	$(CPPGO) check $(GEN_ARGS) $(SCAN_ARGS)

## --- tools and housekeeping ------------------------------------------------

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

clean:
	rm -rf $(BUILD) $(SHIM)
