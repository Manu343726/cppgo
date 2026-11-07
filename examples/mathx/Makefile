# mathx — the direct C++ ABI mode on one small class.
#
#   make              build libmathx.so, generate the bindings, test and vet
#   make check        prove the committed generated files are up to date
#   make inline-demo  show cppgo refusing a member defined inside its class
#   make clean
#
# Everything is built under build/, which is git-ignored: the shared library, the
# cppgo binary and the compilation database. The binary is built from the
# repository this example lives in (../../cmd/cppgo), so there is no install
# step and no chance of testing a different cppgo than the one next door.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
CXX      ?= g++
CXXFLAGS ?= -std=c++17 -O2 -fPIC -Wall -Wextra

BUILD := build
ROOT  := ../..
LIB   := $(BUILD)/libmathx.so
CPPGO := $(BUILD)/cppgo
CDB   := $(BUILD)/compile_commands.json

SOURCES := src/vector.cpp
HEADERS := include/mathx/vector.hpp
INCLUDES := -Iinclude -Iinclude/mathx

# The generator is given the same compilation database the compiler gets, so the
# header is parsed with this project's real flags and the report says
# "abi from compile_commands.json" instead of warning that the flags are a guess.
CPPGO_ARGS := -dir . -compile-commands $(CURDIR)/$(CDB)

.PHONY: all lib gen test vet check inline-demo clean

all: test vet

## --- the C++ side ----------------------------------------------------------

lib: $(LIB)

$(LIB): $(SOURCES) $(HEADERS) | $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -o $(LIB) $(SOURCES)

# What a CMake build writes with CMAKE_EXPORT_COMPILE_COMMANDS=ON. Written by
# hand only because this example does not need a whole build system.
$(CDB): $(SOURCES) $(HEADERS) | $(BUILD)
	@printf '[{"directory": "%s", "arguments": ["%s", "-std=c++17", "-fPIC", "-I%s/include", "-I%s/include/mathx", "-c", "%s/src/vector.cpp"], "file": "%s/src/vector.cpp"}]\n' \
		"$(CURDIR)" "$(CXX)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > $(CDB)

## --- the Go side -----------------------------------------------------------

gen: $(CPPGO) $(CDB)
	$(CPPGO) gen $(CPPGO_ARGS)

test: lib gen
	CPPGO_MATHX_LIB=$(CURDIR)/$(LIB) $(GO) test -count=1 -v .

vet: gen
	$(GO) vet .

check: $(CPPGO) $(CDB)
	$(CPPGO) check $(CPPGO_ARGS)

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

## --- the limitation, on demand ---------------------------------------------

# The header defines norm_squared() inside the class body, so it is implicitly
# inline and the library exports no symbol for it. Bind it and the generator
# must fail the build, with a message that says why — this is the target that
# proves it, on a scratch copy so the real package is untouched.
inline-demo: $(CPPGO) $(CDB)
	@rm -rf $(BUILD)/inline-demo
	@mkdir -p $(BUILD)/inline-demo
	@cp -r include mathx.go $(BUILD)/inline-demo/
	@printf '\n// NormSquared binds the member the header defines inline.\n//cppgo:method norm_squared\nfunc (v Vector) NormSquared() float64\n' \
		>> $(BUILD)/inline-demo/mathx.go
	@if out=$$($(CPPGO) gen -dir $(BUILD)/inline-demo -compile-commands $(CURDIR)/$(CDB) 2>&1); then \
		printf 'FAILED: cppgo gen accepted an inline member:\n%s\n' "$$out"; exit 1; \
	fi; \
	printf 'cppgo gen refused it, as it must:\n'; \
	printf '%s\n' "$$out" | sed 's/^/  /'; \
	printf '%s\n' "$$out" | grep -q 'no symbol' || { \
		printf 'FAILED: it failed for another reason (the message above does not mention "no symbol")\n'; exit 1; }

clean:
	rm -rf $(BUILD)
