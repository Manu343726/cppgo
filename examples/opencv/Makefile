# opencv — bind a slice of OpenCV's core and imgproc headers to Go.
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
# installed by the system, and the shim links against its imgproc shared object,
# which names core among its dependencies. The library and the include root are
# the two paths a machine can differ in, so they are variables: OPENCV_INCLUDE
# and OPENCV_LIB.
#
# On a machine without OpenCV every target prints why it is skipping and exits
# zero, which is what lets scripts/build-examples.sh run on a laptop. Inside the
# CI image the headers and the shared object are there, and the targets generate,
# build, load and test against the real library.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO       ?= go
ROOT     := ../..

OPENCV_INCLUDE ?= /usr/include/opencv4
# The module that carries cvtColor, and the one the shim links against: an
# OpenCV module names libopencv_core among its dependencies, so linking imgproc
# also resolves the core symbols Mat's constructors need. The image-codec module
# is not linked: imread and imwrite are refused, see README.md.
OPENCV_LIB     ?= /usr/lib/x86_64-linux-gnu/libopencv_imgcodecs.so

CORE     := $(OPENCV_INCLUDE)/opencv2/core.hpp
IMGPROC  := $(OPENCV_INCLUDE)/opencv2/imgproc.hpp
IMGCODEC := $(OPENCV_INCLUDE)/opencv2/imgcodecs.hpp
BUILD  := build
CPPGO  := $(BUILD)/cppgo
DB     := $(BUILD)/compile_commands.json
SHIM   := libcppgo_opencv.so

# The declarations this example binds. Each one is a whole declaration, or a
# single member selected by its full signature: the enums, a POD with its two
# constructors, an object with state, the core library's scalar configuration
# functions, and the image pipeline — cv::Mat's carryable constructors and
# fields, cv::_InputArray/cv::_OutputArray built around a Mat, cv::cvtColor and
# cv::norm. What is deliberately absent, and why, is in README.md.
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
        -bind cv::setUseOptimized \
        -bind cv::Mat \
        -bind 'cv::_InputArray::_InputArray()' \
        -bind 'cv::_InputArray::_InputArray(const Mat &)' \
        -bind 'cv::_InputArray::_InputArray(const double &)' \
        -bind 'cv::_OutputArray::_OutputArray()' \
        -bind 'cv::_OutputArray::_OutputArray(Mat &)' \
        -bind cv::ColorConversionCodes \
        -bind cv::InterpolationFlags \
        -bind cv::ThresholdTypes \
        -bind cv::BorderTypes \
        -bind cv::cvtColor \
        -bind cv::threshold \
        -bind cv::resize \
        -bind cv::GaussianBlur \
        -bind cv::blur \
        -bind cv::Canny \
        -bind cv::contourArea \
        -bind cv::boundingRect \
        -bind 'cv::norm(InputArray, int, InputArray)' \
        -bind cv::mean \
        -bind cv::sum \
        -bind cv::minMaxLoc \
        -bind cv::add \
        -bind cv::subtract \
        -bind cv::multiply \
        -bind cv::divide \
        -bind cv::imread \
        -bind cv::imwrite \
        -bind cv::imencode

# The concrete instantiations the slice uses. A member function template has no
# Go spelling (Go has no type parameters on methods), and a class template
# specialization is not a declaration libclang visits unless the header names
# it; the manifest names both, and the emitter writes the arguments into the
# call.
INSTANTIATE := -instantiate 'cv::Mat::at<unsigned char>(int, int)' \
               -instantiate 'cv::Mat::at<double>(int, int)' \
               -instantiate 'cv::Size_<int>' \
               -instantiate 'cv::Scalar_<double>' \
               -instantiate 'cv::Point_<int>' \
               -instantiate 'cv::Rect_<int>'

GEN_ARGS := $(foreach h,$(CORE) $(IMGPROC) $(IMGCODEC),-header $(h)) -package opencv \
            -library $(OPENCV_LIB) -compile-commands $(DB) -out . -coverage coverage.txt \
            $(BIND) $(INSTANTIATE)

TEST_ENV := CPPGO_OPENCV_LIB=$(OPENCV_LIB) CPPGO_OPENCV_SHIM=$(CURDIR)/$(SHIM)

HAVE := $(and $(wildcard $(CORE)),$(wildcard $(IMGPROC)),$(wildcard $(IMGCODEC)),$(wildcard $(OPENCV_LIB)))

.PHONY: all lib gen test vet check coverage clean

ifneq ($(HAVE),)
## The library is installed: the real targets.

all: gen test vet

lib:
	@printf 'opencv: the library is the system %s\n' "$(OPENCV_LIB)"

## The headers' flags come from the compilation database cppgo reads, so the
## parse and the shim compile with the same include root the example does. One
## entry per header: cppgo looks the file up by path.
$(DB): Makefile | $(BUILD)
	@printf '[' > $@
	@sep=; for h in $(CORE) $(IMGPROC) $(IMGCODEC); do \
		printf '%s{"directory":"%s","file":"%s","arguments":["clang++","-std=c++17","-I%s","-xc++-header","%s","-c","%s"]}' \
			"$$sep" "$(CURDIR)" "$$h" "$(OPENCV_INCLUDE)" "$$h" "$$h" >> $@; \
		sep=,; \
	done
	@printf ']\n' >> $@

gen: $(CPPGO) $(DB)
	$(CPPGO) gen $(GEN_ARGS)

## The coverage report is a committed output: regeneration compares it with the
## file on disk, so a declaration that stops binding shows up as a diff and the
## targets job fails on it.
coverage: $(CPPGO) $(DB)
	$(CPPGO) check $(GEN_ARGS)

# The staleness check runs before gen on purpose: gen rewrites the files, so a
# check after it could never fail. The targets job runs this target on the
# checked-out tree, which is where a declaration that stopped binding shows.
test: coverage gen
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
	@printf 'opencv: %s is not installed — skipping (install libopencv-dev, or set OPENCV_INCLUDE and OPENCV_LIB)\n' "$(CORE)"

clean:
	rm -rf $(BUILD) $(SHIM)

endif
