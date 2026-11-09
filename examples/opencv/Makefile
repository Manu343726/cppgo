# opencv — bind a slice of OpenCV's core header to Go.
#
#   make        generate the binding, test and vet
#   make lib    report the OpenCV shared object the binding links against
#   make gen    cppgo gen: the Go API, the assembly, the shim sources and
#               libcppgo_opencv.so
#   make test   go test with -tags cppgo_opencv, once with CGO_ENABLED=0 and
#               once with the race detector
#   make vet    go vet with the example's build tag
#   make check  prove the committed generated sources are up to date
#   make clean
#
# There is no user library to build here: the C++ library is OpenCV itself,
# installed by the system, and the shim links against its core shared object.
# The library and the include root are the two paths a machine can differ in, so
# they are variables: OPENCV_INCLUDE and OPENCV_LIB.
#
# On a machine without OpenCV every target prints why it is skipping and exits
# zero, which is what lets scripts/build-examples.sh run on a laptop. Inside the
# CI image the header and the shared object are there, and the targets generate,
# build, load and test against the real library.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
ROOT     := ../..

OPENCV_INCLUDE ?= /usr/include/opencv4
OPENCV_LIB     ?= /usr/lib/x86_64-linux-gnu/libopencv_core.so

HEADER := $(OPENCV_INCLUDE)/opencv2/core.hpp
BUILD  := build
CPPGO  := $(BUILD)/cppgo
DB     := $(BUILD)/compile_commands.json
SHIM   := libcppgo_opencv.so

# The declarations this example binds. Each one is a whole declaration, or a
# record and everything in it: an enum, a POD with its two constructors and its
# field accessors, an object with state, and the core library's scalar
# configuration functions. What is deliberately absent, and why, is in README.md.
BIND := -bind cv::NormTypes \
        -bind cv::TermCriteria \
        -bind cv::TickMeter \
        -bind cv::getVersionMajor \
        -bind cv::getVersionMinor \
        -bind cv::getTickFrequency \
        -bind cv::getNumThreads \
        -bind cv::setNumThreads \
        -bind cv::getNumberOfCPUs \
        -bind cv::useOptimized \
        -bind cv::setUseOptimized

GEN_ARGS := -header $(HEADER) -package opencv -library $(OPENCV_LIB) \
            -compile-commands $(DB) -out . $(BIND)

TEST_ENV := CPPGO_OPENCV_LIB=$(OPENCV_LIB) CPPGO_OPENCV_SHIM=$(CURDIR)/$(SHIM)

HAVE := $(and $(wildcard $(HEADER)),$(wildcard $(OPENCV_LIB)))

.PHONY: all lib gen test vet check clean

ifneq ($(HAVE),)
## The library is installed: the real targets.

all: gen test vet

lib:
	@printf 'opencv: the library is the system %s\n' "$(OPENCV_LIB)"

## The header's flags come from the compilation database cppgo reads, so the
## parse and the shim compile with the same include root the example does.
$(DB): Makefile | $(BUILD)
	@printf '[{"directory":"%s","file":"%s","arguments":["clang++","-std=c++17","-I%s","-xc++-header","%s","-c","%s"]}]\n' \
		"$(CURDIR)" "$(HEADER)" "$(OPENCV_INCLUDE)" "$(HEADER)" "$(HEADER)" > $@

gen: $(CPPGO) $(DB)
	$(CPPGO) gen $(GEN_ARGS)

test: gen
	CGO_ENABLED=0 $(TEST_ENV) $(GO) test -tags cppgo_opencv -count=1 -v .
	CGO_ENABLED=1 $(TEST_ENV) $(GO) test -tags cppgo_opencv -race -count=1 .

vet: gen
	CGO_ENABLED=0 $(GO) vet -tags cppgo_opencv .

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
	@printf 'opencv: %s is not installed — skipping (install libopencv-dev, or set OPENCV_INCLUDE and OPENCV_LIB)\n' "$(HEADER)"

clean:
	rm -rf $(BUILD) $(SHIM)

endif
