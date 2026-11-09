# eigen — bind a slice of Eigen's Dense header to Go.
#
#   make        generate the binding, test and vet
#   make lib    report that Eigen has no shared object to build or link
#   make gen    cppgo gen: the Go API, the assembly, the shim sources and
#               libcppgo_eigen.so
#   make test   go test with -tags cppgo_eigen, once with CGO_ENABLED=0 and once
#               with the race detector
#   make vet    go vet with the example's build tag
#   make check  prove the committed generated sources are up to date
#   make clean
#
# Eigen is header-only: there is no library to build and none to link, so the
# shim cppgo compiles is the whole binary. The include root is the one path a
# machine can differ in, so it is a variable: EIGEN_INCLUDE.
#
# On a machine without Eigen every target prints why it is skipping and exits
# zero, which is what lets scripts/build-examples.sh run on a laptop.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
ROOT     := ../..

EIGEN_INCLUDE ?= /usr/include/eigen3

HEADER := $(EIGEN_INCLUDE)/Eigen/Dense
BUILD  := build
CPPGO  := $(BUILD)/cppgo
DB     := $(BUILD)/compile_commands.json
SHIM   := libcppgo_eigen.so

# The declarations this example binds: Eigen's three configuration enums and the
# functions that report and set the thread count its parallel algorithms use.
# What is absent, and why, is in README.md.
BIND := -bind Eigen::StorageOptions \
        -bind Eigen::ComputationInfo \
        -bind Eigen::UpLoType \
        -bind Eigen::nbThreads \
        -bind Eigen::setNbThreads \
        -bind Eigen::initParallel

GEN_ARGS := -header $(HEADER) -package eigen \
            -compile-commands $(DB) -out . $(BIND)

TEST_ENV := CPPGO_EIGEN_SHIM=$(CURDIR)/$(SHIM)

HAVE := $(wildcard $(HEADER))

.PHONY: all lib gen test vet check clean

ifneq ($(HAVE),)
## Eigen is installed: the real targets.

all: gen test vet

lib:
	@printf 'eigen: header-only, no shared object — the shim is the whole binary\n'

## The header's flags come from the compilation database cppgo reads, so the
## parse and the shim compile with the same include root the example does.
$(DB): Makefile | $(BUILD)
	@printf '[{"directory":"%s","file":"%s","arguments":["clang++","-std=c++17","-I%s","-xc++-header","%s","-c","%s"]}]\n' \
		"$(CURDIR)" "$(HEADER)" "$(EIGEN_INCLUDE)" "$(HEADER)" "$(HEADER)" > $@

gen: $(CPPGO) $(DB)
	$(CPPGO) gen $(GEN_ARGS)

test: gen
	CGO_ENABLED=0 $(TEST_ENV) $(GO) test -tags cppgo_eigen -count=1 -v .
	CGO_ENABLED=1 $(TEST_ENV) $(GO) test -tags cppgo_eigen -race -count=1 .

vet: gen
	CGO_ENABLED=0 $(GO) vet -tags cppgo_eigen .

check: $(CPPGO) $(DB)
	$(CPPGO) check $(GEN_ARGS)

clean:
	rm -rf $(BUILD) $(SHIM)

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

else
## Eigen is not installed: skip, with the reason, and exit zero.

all lib gen test vet check:
	@printf 'eigen: %s is not installed — skipping (install libeigen3-dev, or set EIGEN_INCLUDE)\n' "$(HEADER)"

clean:
	rm -rf $(BUILD) $(SHIM)

endif
