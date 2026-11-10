# fingerprint — the complete application example: a Go program driving a C++
# library that does all of the image work.
#
#   make          install the C++ dependencies, build the library, generate the
#                 binding, run the C++ tests, the Go tests and the benchmarks
#   make conan    `conan install` only: the pinned spdlog, into build/conan
#   make lib      the C++ library only, by CMake, with Conan's toolchain
#   make fixtures regenerate testdata/ from tools/make_fixtures.cpp
#   make cpp-test the C++ tests, through CTest
#   make gen      cppgo gen: the Go API, the assembly, the shim sources and
#                 gen/libcppgo_fingerprint.so
#   make app      go build ./cmd/fingerprint into build/fingerprint
#   make test     the C++ tests, then go test twice (no cgo, then the race detector)
#   make bench    the trigger and the full pipeline, ns per frame
#   make vet      go vet
#   make run      build and start the program
#   make check    prove the committed generated sources are current
#   make clean
#
# The C++ dependencies are Conan's and the build system is CMake, as the rest of
# the repository's examples are built: `conan install` writes the toolchain and
# the package's CMake files under build/conan, and CMake is pointed at them.
# OpenCV is the image's own installation and is found by path, because its
# component packages ship no OpenCVConfig.cmake (see the Dockerfile).
#
# On a machine without Conan every target prints why it is skipping and exits
# zero, which is what lets scripts/build-examples.sh run on a host that has no
# Python. The CI image carries Python 3 and Conan from #135 on; before that
# image is published, this example is verified on a host with Conan and in a
# locally built image that adds it.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := all

GO      ?= go
CMAKE   ?= cmake
CTEST   ?= ctest
CONAN   ?= conan
BUILD_TYPE ?= Release

ROOT  := ../..
BUILD := build
GEN   := gen
CPPGO := $(BUILD)/cppgo
LIB   := $(BUILD)/libfingerprint.so
APP   := $(BUILD)/fingerprint
SHIM  := $(GEN)/libcppgo_fingerprint.so

CONAN_OUTPUT  := $(BUILD)/conan
TOOLCHAIN     := $(CONAN_OUTPUT)/conan_toolchain.cmake
CONAN_LOCK    := conan.lock
CONAN_PROFILE_ARGS := -s build_type=$(BUILD_TYPE) -s compiler.cppstd=17

OPENCV_INCLUDE ?= /usr/include/opencv4
OPENCV_LIB_DIR ?= /usr/lib/x86_64-linux-gnu

HEADER := include/fingerprint/fingerprint.hpp

# The exact generation command, with paths relative to this directory, so the
# command in the generated file's banner is one a reader can copy. The flags for
# the header come from the CMake compilation database.
GEN_ARGS := -header $(HEADER) -package fingerprint -library $(LIB) \
            -compile-commands $(BUILD)/compile_commands.json -out ./$(GEN)

HAVE_CONAN  := $(shell command -v $(CONAN) 2>/dev/null)
HAVE_CMAKE  := $(shell command -v $(CMAKE) 2>/dev/null)
HAVE_OPENCV := $(and $(wildcard $(OPENCV_INCLUDE)/opencv2/core.hpp),$(wildcard $(OPENCV_LIB_DIR)/libopencv_core.so))
# All three or nothing: each one is a dependency this example cannot fake.
HAVE_ALL    := $(and $(HAVE_CONAN),$(HAVE_CMAKE),$(HAVE_OPENCV))

.PHONY: all conan configure lib fixtures cpp-test gen app test bench vet run check clean

ifneq ($(strip $(HAVE_ALL)),)

all: app test vet bench

## --- the C++ dependencies and the library ----------------------------------

conan: $(TOOLCHAIN)

# The lockfile pins every recipe revision the example was built with, so a
# machine that installs later gets the same spdlog and the same fmt.
$(TOOLCHAIN): conanfile.txt $(CONAN_LOCK) | $(BUILD)
	@# Conan needs a profile, and the profile names the host's compiler: it is
	@# detected on a machine that has none, never committed.
	@if [ ! -f "$$HOME/.conan2/profiles/default" ]; then $(CONAN) profile detect; fi
	$(CONAN) install conanfile.txt -of $(CONAN_OUTPUT) --lockfile=$(CONAN_LOCK) $(CONAN_PROFILE_ARGS)
	@test -f $@ || { echo "conan did not write $@"; exit 1; }

configure: $(TOOLCHAIN)
	$(CMAKE) -S . -B $(BUILD) \
		-DCMAKE_TOOLCHAIN_FILE=$(CURDIR)/$(TOOLCHAIN) \
		-DCMAKE_BUILD_TYPE=$(BUILD_TYPE) \
		-DOPENCV_INCLUDE=$(OPENCV_INCLUDE) \
		-DOPENCV_LIB_DIR=$(OPENCV_LIB_DIR)

lib: $(LIB)

$(LIB): configure $(wildcard src/*.cpp) $(wildcard include/fingerprint/*.hpp) CMakeLists.txt
	$(CMAKE) --build $(BUILD) --target fingerprint

fixtures: lib
	$(CMAKE) --build $(BUILD) --target fingerprint_fixtures
	./$(BUILD)/fingerprint_fixtures testdata

cpp-test: lib
	$(CMAKE) --build $(BUILD) --target fingerprint_tests
	cd $(BUILD) && $(CTEST) --output-on-failure --no-tests=error

## --- the generated binding -------------------------------------------------

gen: $(CPPGO) $(LIB)
	$(CPPGO) gen $(GEN_ARGS)

app: gen
	CGO_ENABLED=0 $(GO) build -o $(APP) ./cmd/fingerprint

## --- the Go program and the tests ------------------------------------------

test: cpp-test gen
	CGO_ENABLED=0 $(GO) test -count=1 -v ./...
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

bench: gen
	$(GO) test -run '^$$' -bench . -benchmem ./cmd/fingerprint

vet: gen
	CGO_ENABLED=0 $(GO) vet ./...

run: app
	./$(APP)

check: $(CPPGO) $(LIB)
	$(CPPGO) check $(GEN_ARGS)

## --- tools and housekeeping ------------------------------------------------

$(CPPGO): | $(BUILD)
	cd $(ROOT) && CGO_ENABLED=0 $(GO) build -trimpath -o $(CURDIR)/$(CPPGO) ./cmd/cppgo

$(BUILD):
	@mkdir -p $(BUILD)

clean:
	rm -rf $(BUILD) $(SHIM)

else
## Conan, CMake or OpenCV is missing: skip, with the reason, and exit zero.

all conan configure lib fixtures cpp-test gen app test bench vet run check:
	@printf 'fingerprint: skipping — '
	@if [ -z '$(strip $(HAVE_CONAN))' ]; then \
		printf 'conan is not installed (the CI image gains Python 3 and Conan in #135)\n'; \
	elif [ -z '$(strip $(HAVE_CMAKE))' ]; then \
		printf 'cmake is not installed\n'; \
	else \
		printf '%s is not installed (install libopencv-dev, or set OPENCV_INCLUDE and OPENCV_LIB_DIR)\n' \
			'$(OPENCV_INCLUDE)/opencv2/core.hpp'; \
	fi

clean:
	rm -rf $(BUILD) $(SHIM)

endif
