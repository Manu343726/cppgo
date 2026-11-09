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

ARRAY     := $(AF_INCLUDE)/af/array.h
DATA      := $(AF_INCLUDE)/af/data.h
ALGORITHM := $(AF_INCLUDE)/af/algorithm.h
BLAS      := $(AF_INCLUDE)/af/blas.h
LAPACK    := $(AF_INCLUDE)/af/lapack.h
HEADERS   := $(ARRAY) $(DATA) $(ALGORITHM) $(BLAS) $(LAPACK)
BUILD  := build
CPPGO  := $(BUILD)/cppgo
DB     := $(BUILD)/compile_commands.json
SHIM   := libcppgo_arrayfire.so

# The declarations this example binds: the type enums, the device functions,
# af::array's carryable constructors and its shape and dtype accessors, the
# value-producing constructors af::identity and af::range, the reductions
# af::sum/af::max/af::min and af::norm, af::matmul, af::transpose and af::seq.
# Each one is a full signature where the overload set cannot bind whole; what is
# absent, and why, is in README.md — including af::exception and af::infoString,
# which the generator reports as bound but the generated Go does not compile.
BIND := -bind af_dtype \
        -bind af_source \
        -bind af_norm_type \
        -bind af::getDevice \
        -bind af::getDeviceCount \
        -bind af::getMemStepSize \
        -bind af::getSizeOf \
        -bind af::isDoubleAvailable \
        -bind af::isHalfAvailable \
        -bind af::seq \
        -bind 'af::dim4::dim4(dim_t, dim_t, dim_t, dim_t)' \
        -bind 'af::array::array()' \
        -bind 'af::array::array(dim_t, dtype)' \
        -bind 'af::array::array(dim_t, dim_t, dtype)' \
        -bind af::array::type \
        -bind af::array::dims \
        -bind af::array::elements \
        -bind af::array::as \
        -bind af::array::isempty \
        -bind 'af::identity(const dim_t, const dim_t, const dtype)' \
        -bind 'af::range(const dim4 &, const int, const dtype)' \
        -bind 'af::sum(const array &, const int)' \
        -bind 'af::sum(const array &, const int, const double)' \
        -bind 'af::max(const array &, const int)' \
        -bind 'af::min(const array &, const int)' \
        -bind 'af::matmul(const array &, const array &, const matProp, const matProp)' \
        -bind 'af::transpose(const array &, const bool)' \
        -bind 'af::norm(const array &, const normType, const double, const double)'

GEN_ARGS := $(foreach h,$(HEADERS),-header $(h)) -package arrayfire -library $(AF_LIB) \
            -compile-commands $(DB) -out . $(BIND)

TEST_ENV := CPPGO_ARRAYFIRE_LIB=$(AF_LIB) CPPGO_ARRAYFIRE_SHIM=$(CURDIR)/$(SHIM) \
            LD_LIBRARY_PATH=$(dir $(AF_LIB))

HAVE := $(and $(wildcard $(ARRAY)),$(wildcard $(AF_LIB)))

.PHONY: all lib gen test vet check clean

ifneq ($(HAVE),)
## The library is installed: the real targets.

all: gen test vet

lib:
	@printf 'arrayfire: the library is %s\n' "$(AF_LIB)"

## The header's flags come from the compilation database cppgo reads, so the
## parse and the shim compile with the same include root the example does.
## The headers' flags come from the compilation database cppgo reads, so the
## parse and the shim compile with the same include root the example does. One
## entry per header: cppgo looks the file up by path.
$(DB): Makefile | $(BUILD)
	@printf '[' > $@
	@sep=; for h in $(HEADERS); do \
		printf '%s{"directory":"%s","file":"%s","arguments":["clang++","-std=c++17","-I%s","-xc++-header","%s","-c","%s"]}' \
			"$$sep" "$(CURDIR)" "$$h" "$(AF_INCLUDE)" "$$h" "$$h" >> $@; \
		sep=,; \
	done
	@printf ']\n' >> $@

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
	@printf 'arrayfire: %s is not installed — skipping (install libarrayfire-dev, or set AF_INCLUDE and AF_LIB)\n' "$(ARRAY)"

clean:
	rm -rf $(BUILD) $(SHIM)

endif
