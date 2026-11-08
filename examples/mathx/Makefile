# mathx — the v4 header-first flow on one small C++17 class.
#
#   make        build the user's library, generate the binding, test and vet
#   make lib    build build/libmathx.so with the project's own command
#   make gen    run cppgo gen: the Go API, the assembly, the shim sources and
#               the shim libcppgo_mathx.so
#   make test   go test, once with CGO_ENABLED=0 and once with the race detector
#   make check  prove the committed generated sources are up to date
#   make clean
#
# Everything is built under build/, which is git-ignored: the user's shared
# object and the locally built cppgo. The shim binary cppgo writes beside the
# package (libcppgo_mathx.so) is ignored too. The generated *sources* are
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
LIB    := $(BUILD)/libmathx.so
CPPGO  := $(BUILD)/cppgo
SHIM   := libcppgo_mathx.so

HEADER   := include/mathx/vector.hpp
SOURCE   := src/vector.cpp
INCLUDES := -Iinclude

# The exact generation command, with paths relative to this directory, so the
# command in the generated file's banner is one a reader can copy. The .clangd
# beside this Makefile is the project's compilation configuration: -std=c++17
# and -Iinclude, which is what tells the parser how the header is built.
GEN_ARGS := -header $(HEADER) -package mathx -library $(LIB) -out .

TEST_ENV := CPPGO_MATHX_LIB=$(CURDIR)/$(LIB) CPPGO_MATHX_SHIM=$(CURDIR)/$(SHIM)

.PHONY: all lib gen test vet check clean

all: gen test vet

## --- the user's library ----------------------------------------------------
#
# This is the whole of the C++ build: the user's compiler, the user's flags and
# a soname. cppgo is not involved, and no generated file is compiled here.

lib: $(LIB)

$(LIB): $(SOURCE) $(HEADER) | $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -Wl,-soname,libmathx.so -o $@ $(SOURCE)

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

## --- tools and housekeeping ------------------------------------------------

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

clean:
	rm -rf $(BUILD) $(SHIM)
