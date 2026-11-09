# arrayfire — bind a slice of ArrayFire's C++ header to Go.
#
#   make        generate the binding, test and vet
#   make lib    report the ArrayFire shared object the binding links against
#   make gen    cppgo gen: the Go API, the assembly, the shim sources and
#               libcppgo_arrayfire.so
#   make test   go test with -tags cppgo_arrayfire, once with CGO_ENABLED=0 and
#               once with the race detector
#   make vet    go vet with the example's build tag
#   make check  prove the committed generated sources are up to date
#   make clean
#
# There is no user library to build here: the C++ library is ArrayFire itself.
# The shim links against libaf.so, which is ArrayFire's CPU backend in the CI
# image. The include root and the library are the two paths a machine can differ
# in, so they are variables: AF_INCLUDE and AF_LIB.
#
# On a machine without ArrayFire every target prints why it is skipping and exits
# zero, which is what lets scripts/build-examples.sh run on a laptop.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
ROOT     := ../..

AF_INCLUDE ?= /usr/local/include
AF_LIB     ?= /usr/local/lib/libaf.so

HEADER := $(AF_INCLUDE)/af/array.h
BUILD  := build
CPPGO  := $(BUILD)/cppgo
DB     := $(BUILD)/compile_commands.json
SHIM   := libcppgo_arrayfire.so

# The declarations this example binds: ArrayFire's two type enums and the
# scalar functions its device API reports. What is absent, and why, is in
# README.md.
BIND := -bind af_dtype \
        -bind af_source \
        -bind af::getDevice \
        -bind af::getDeviceCount \
        -bind af::getMemStepSize \
        -bind af::getSizeOf \
        -bind af::isDoubleAvailable \
        -bind af::isHalfAvailable

GEN_ARGS := -header $(HEADER) -package arrayfire -library $(AF_LIB) \
            -compile-commands $(DB) -out . $(BIND)

TEST_ENV := CPPGO_ARRAYFIRE_LIB=$(AF_LIB) CPPGO_ARRAYFIRE_SHIM=$(CURDIR)/$(SHIM) \
            LD_LIBRARY_PATH=$(dir $(AF_LIB))

HAVE := $(and $(wildcard $(HEADER)),$(wildcard $(AF_LIB)))

.PHONY: all lib gen test vet check clean

ifneq ($(HAVE),)
## The library is installed: the real targets.

all: gen test vet

lib:
	@printf 'arrayfire: the library is %s\n' "$(AF_LIB)"

## The header's flags come from the compilation database cppgo reads, so the
## parse and the shim compile with the same include root the example does.
$(DB): Makefile | $(BUILD)
	@printf '[{"directory":"%s","file":"%s","arguments":["clang++","-std=c++17","-I%s","-xc++-header","%s","-c","%s"]}]\n' \
		"$(CURDIR)" "$(HEADER)" "$(AF_INCLUDE)" "$(HEADER)" "$(HEADER)" > $@

gen: $(CPPGO) $(DB)
	$(CPPGO) gen $(GEN_ARGS)

test: gen
	CGO_ENABLED=0 $(TEST_ENV) $(GO) test -tags cppgo_arrayfire -count=1 -v .
	CGO_ENABLED=1 $(TEST_ENV) $(GO) test -tags cppgo_arrayfire -race -count=1 .

vet: gen
	CGO_ENABLED=0 $(GO) vet -tags cppgo_arrayfire .

check: $(CPPGO) $(DB)
	$(CPPGO) check $(GEN_ARGS)

clean:
	rm -rf $(BUILD) $(SHIM)

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

else
## The library is not installed: skip, with the reason, and exit zero.

all lib gen test vet check:
	@printf 'arrayfire: %s is not installed — skipping (install libarrayfire-dev, or set AF_INCLUDE and AF_LIB)\n' "$(HEADER)"

clean:
	rm -rf $(BUILD) $(SHIM)

endif
