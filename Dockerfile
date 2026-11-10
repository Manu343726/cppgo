# syntax=docker/dockerfile:1
#
# cppgo CI image — published as ghcr.io/manu343726/cppgo-ci.
#
# Why this exists: CI used to inherit its toolchain from ubuntu-latest. When the
# runner image stopped shipping libclang, internal/cxx silently skipped itself,
# its coverage vanished and the 99% gate went red for a reason that had nothing
# to do with the code. Everything that suite needs is pinned here instead.
#
# Base: debian:trixie-slim. Bookworm is NOT usable, and this was measured, not
# assumed: Debian 12 ships clang 14, and with it the suite fails (cmd/cppgo's
# ABI tests — TestGenABIReport, TestCheckABIIsSilent, TestGenABIForce, ... — and
# parts of internal/cxx) because the libclang behaviour the project relies on
# is newer than 14. g++ 14 covers the C++17 fixtures.
#
# clang/libclang come from apt.llvm.org rather than from trixie's archive, and
# that is the same measurement a second time. trixie's archive stops at clang 19,
# and since eadb901 (2026-11-08) the tree binds `clang_getOffsetOfBase`, which
# libclang 19 does not export — the suite dies at load time with
#
#   cxx: libclang not found: … /usr/lib/libclang.so: undefined symbol:
#   clang_getOffsetOfBase
#
# which is a statement about the image and not about the code under test: the
# failure this image exists to prevent.
#
# Why 22 and not "the oldest major that exports the symbol": both halves are
# measured. LLVM 20 is the oldest that exports `clang_getOffsetOfBase`
# (`nm -D --defined-only`: present in 20.1 and 22.1, absent in 19.1). But the
# suite also asserts what libclang *does* with an explicitly instantiated
# template — internal/cxx expects the layout to be deferred (`LayoutKnown` false,
# size -1) — and that expectation only holds while the parse cannot resolve
# `<cstddef>`. With the C++ headers available, libclang 20, 21 and 22 all
# complete the layout and four tests in internal/cxx fail; with `-nostdinc++`
# the same binary reports the deferred layout the tests expect. So it is the
# tests that encode a degraded parse, not a property of a major, and the finding
# is named in the pull request that introduced this pin. No compiler here can
# satisfy both the test and the code, so the pin follows the code.
#
# 22.1.8 is the version the project is developed against, and apt.llvm.org's
# trixie archive ships it as a release build (the -20 and -21 components are
# snapshots of their branches). Its signing key is fingerprint-checked below
# instead of trusted, and the build-time probe proves `clang_getOffsetOfBase` is
# there, so a pin that falls behind the code fails the build rather than the
# test suite.
#
# The target libraries (#15). These exist so the target-use-case tests run against
# the real libraries and not against mocks, and each one's provenance is written
# here because it decides how a bump is done:
#
#   Eigen 3.4.0        package `libeigen3-dev` (trixie). Header-only, no runtime.
#   OpenCV 4.10.0      the component -dev packages of every headless module
#                      issue #132 binds (trixie): core, imgproc, imgcodecs,
#                      video, calib3d, features2d, objdetect, photo, flann, ml,
#                      dnn and stitching. The component -dev packages and NOT
#                      the `libopencv-dev` metapackage: the metapackage pulls
#                      the whole GUI chain (Qt5, GTK, Vulkan, OpenCL ICD) for
#                      `highgui`, which this image has no use for, and the
#                      codec stack (ffmpeg, gstreamer) for `videoio`, which no
#                      binding here reads a stream from. The consequence, said
#                      plainly: OpenCV's own `opencv4.pc` and
#                      `OpenCVConfig.cmake` ship only in that metapackage, so
#                      the interface here is the header tree plus the component
#                      libraries:
#
#                        -I/usr/include/opencv4
#                        -lopencv_core -lopencv_imgproc -lopencv_imgcodecs
#                        -lopencv_video -lopencv_calib3d -lopencv_features2d
#                        -lopencv_objdetect -lopencv_photo -lopencv_flann
#                        -lopencv_ml -lopencv_dnn -lopencv_stitching
#
#                      `pkg-config --modversion opencv4` therefore does NOT
#                      work in this image, by choice; `dpkg-query -W
#                      libopencv-core-dev` names the version instead.
#   ArrayFire 3.10.0   built from source in the `arrayfire` stage below: trixie's
#                      archive has no ArrayFire package at all (`apt-cache policy
#                      libarrayfire-dev` resolves nothing), so there is nothing to
#                      install. CPU backend only — no CUDA, no OpenCL, no oneAPI,
#                      no graphics, no image IO, no tests, no examples — installed
#                      to /usr/local as `libaf` (unified) plus `libafcpu`.
#
# Qt is out of scope and is not installed anywhere in this image.
#
# Runs as root, deliberately: GitHub Actions container jobs start as root, so a
# non-root USER here would either be ignored (the runner passes --user) or make
# the bind-mounted workspace unwritable. No HEALTHCHECK either: this is a build
# tool, not a service, and a health probe would only add a process to a
# one-shot `docker run`.

ARG DEBIAN_VERSION=trixie

# ---------------------------------------------------------------------------
# Stage 1 — fetch the pinned release artefacts. Nothing here reaches the final
# image except the extracted binaries; curl, the tarballs and apt lists stay in
# this stage.
# ---------------------------------------------------------------------------
FROM debian:${DEBIAN_VERSION}-slim AS fetch

# Go has to be at least the `go` directive in go.mod (1.25.0) — the final image
# sets GOTOOLCHAIN=local, so a module newer than this pin fails loudly instead of
# silently downloading a second toolchain — and at least what the suite itself
# asks for. It asks for more than go.mod does: internal/gen's end-to-end fixture
# writes a throwaway module declaring `go 1.27.0` (writeModule, in
# internal/gen/e2e_test.go), so a 1.25 toolchain stops four of those tests with
# `go.mod requires go >= 1.27.0 (running go 1.25.0; GOTOOLCHAIN=local)`. 1.27.3
# is the newest release, and the `build with the latest Go` job already proves
# the module builds and vets with it. That a test fixture requires a Go newer
# than the library's own documented minimum is a finding in the test, named in
# the pull request that introduced this pin — it is not a reason to hold the
# image behind the suite it has to run.
ARG GO_VERSION=1.27.3
# Same version as .golangci.yml / golangci-lint-action in ci.yml (v2, a v1
# binary refuses the v2 config).
ARG GOLANGCI_LINT_VERSION=v2.15.0
# Same version as the Pages workflow pins.
ARG HUGO_VERSION=0.169.0

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /staging

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
      amd64) goarch=amd64; lintarch=amd64; hugoarch=amd64 ;; \
      arm64) goarch=arm64; lintarch=arm64; hugoarch=arm64 ;; \
      *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --retry 3 --retry-delay 2 -o go.tgz \
      "https://go.dev/dl/go${GO_VERSION}.linux-${goarch}.tar.gz"; \
    mkdir -p rootfs/usr/local; \
    tar -xzf go.tgz -C rootfs/usr/local; \
    rm -f go.tgz; \
    \
    curl -fsSL --retry 3 --retry-delay 2 -o golangci-lint.tgz \
      "https://github.com/golangci/golangci-lint/releases/download/${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-${lintarch}.tar.gz"; \
    tar -xzf golangci-lint.tgz --strip-components=1 \
      -C rootfs "golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-${lintarch}/golangci-lint"; \
    rm -f golangci-lint.tgz; \
    mkdir -p rootfs/usr/local/bin; \
    mv rootfs/golangci-lint rootfs/usr/local/bin/golangci-lint; \
    \
    curl -fsSL --retry 3 --retry-delay 2 -o hugo.tgz \
      "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${hugoarch}.tar.gz"; \
    tar -xzf hugo.tgz -C rootfs hugo; \
    rm -f hugo.tgz; \
    mv rootfs/hugo rootfs/usr/local/bin/hugo; \
    chmod 0755 rootfs/usr/local/bin/golangci-lint rootfs/usr/local/bin/hugo

# apt.llvm.org's signing key, imported only if its fingerprint is the one this
# repository pinned: an archive that starts serving a different key fails this
# build instead of being trusted. `gpg` is needed to read the key and for nothing
# else, which is why the key is fetched here and copied into the final stage
# rather than installed there — and why this is a step of its own, after the
# pinned downloads: it changes nothing those steps read, so their layer survives.
# Named "fingerprint" and not "key", for the build check that flags any ARG or ENV
# whose name says "key": this value is a public fingerprint, and the flag it would
# raise on every build is noise.
ARG LLVM_ARCHIVE_FINGERPRINT=6084F3CF814B57C1CF12EFD515CF4D18AF4F7421
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends gnupg; \
    curl -fsSL --retry 3 --retry-delay 2 -o apt.llvm.org.asc \
      https://apt.llvm.org/llvm-snapshot.gpg.key; \
    fpr="$(gpg --show-keys --with-colons ./apt.llvm.org.asc | awk -F: '/^fpr:/ {print $10; exit}')"; \
    if [ "$fpr" != "${LLVM_ARCHIVE_FINGERPRINT}" ]; then \
      echo "apt.llvm.org key fingerprint is '$fpr', expected '${LLVM_ARCHIVE_FINGERPRINT}'" >&2; \
      exit 1; \
    fi; \
    rm -rf /var/lib/apt/lists/*

# ---------------------------------------------------------------------------
# Stage 1c — ArrayFire, CPU backend only, from source. Trixie ships no package
# (`apt-cache policy libarrayfire-dev` is empty), and #15 needs the real library,
# so it is built here from a pinned release tag and the final image takes only
# its installed tree. Nothing in this stage reaches the final image except
# /staging/arrayfire.
# ---------------------------------------------------------------------------
FROM debian:${DEBIAN_VERSION}-slim AS arrayfire

# Pinned release, and the only floating thing about it is the tarball itself:
# codeload generates an archive per request, so a checksum of it is not stable
# and pinning one would only document a number that changes. The tag is the pin.
ARG ARRAYFIRE_VERSION=v3.10.0

# The CPU backend links FFTW for its FFT and BLAS/LAPACK for its linear algebra,
# so both are real dependencies here and not niceties: without them ArrayFire
# builds with its slow internal fallbacks or fails outright. Boost's headers are
# a configure-time requirement of the CPU backend's sort-by-key kernels: without
# them CMake reports `Found relative path while evaluating include directories
# … "Boost_INCLUDE_DIR-NOTFOUND"` for every one of them (measured), so
# `libboost-dev` is here for the headers and nothing else. `liblapacke-dev` is
# the C interface the CPU backend's `lapack_helper.hpp` includes directly — the
# Fortran `liblapack-dev` alone stops the compile at `lapacke.h: No such file`.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      cmake \
      curl \
      g++ \
      git \
      libboost-dev \
      libfftw3-dev \
      liblapack-dev \
      liblapacke-dev \
      libopenblas-dev \
      make \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN set -eux; \
    curl -fsSL --retry 3 --retry-delay 2 -o arrayfire.tar.gz \
      "https://codeload.github.com/arrayfire/arrayfire/tar.gz/refs/tags/${ARRAYFIRE_VERSION}"; \
    tar -xzf arrayfire.tar.gz --strip-components=1; \
    rm -f arrayfire.tar.gz

# `git` is not for this recipe: ArrayFire's own CMake clones `forge` (its windowing
# dependency, pinned by the tree at v1.0.8) while configuring, even with
# `AF_BUILD_FORGE=OFF`, which only switches off *building* it. Without git the
# configure dies with `could not find git for clone of af_forge-populate`
# (measured). It also fetches boost_compute, so this stage is not hermetic: both
# clones are pinned by REFs inside the tarball, and both stay in this stage.
#
# BUILD_TESTING=OFF is load-bearing, not tidiness: ArrayFire's test tree downloads
# sparse datasets from the network at configure time and fails the build when it
# cannot (measured on the first attempt, with the error coming out of
# test/CMakeModules/download_sparse_datasets.cmake). AF_BUILD_GRAPHICS is not
# even offered as an option in 3.10; forge is only pulled in by AF_BUILD_FORGE,
# which stays OFF.
# CMAKE_INSTALL_PREFIX is /usr/local — where the files really end up — and the
# staged copy is produced with DESTDIR. That is not a style choice: ArrayFire's
# unified `libaf` resolves its backends through the install prefix compiled into
# it, so staging with `--prefix /staging/arrayfire` leaves it looking for
# `libafcpu` under a directory that only existed during the build, and the first
# `af::constant` dies with `ArrayFire couldn't locate any backends` (measured).
RUN cmake -S . -B build \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_INSTALL_PREFIX=/usr/local \
      -DBUILD_TESTING=OFF \
      -DAF_BUILD_CPU=ON \
      -DAF_BUILD_CUDA=OFF \
      -DAF_BUILD_OPENCL=OFF \
      -DAF_BUILD_ONEAPI=OFF \
      -DAF_BUILD_UNIFIED=ON \
      -DAF_BUILD_EXAMPLES=OFF \
      -DAF_BUILD_DOCS=OFF \
      -DAF_WITH_IMAGEIO=OFF \
      -DAF_BUILD_FORGE=OFF \
 && cmake --build build --parallel "$(nproc)" \
 && DESTDIR=/staging/arrayfire cmake --install build

# ---------------------------------------------------------------------------
# Stage 2 — the image CI runs in.
# ---------------------------------------------------------------------------
FROM debian:${DEBIAN_VERSION}-slim

# Pinned clang/libclang major, from apt.llvm.org (see the base note above).
# trixie's own metapackages (`clang`, `libclang-dev`) stop at 19 and cannot bind
# `clang_getOffsetOfBase`, so they are not used: changing this number is a code
# decision, not an image tweak.
ARG CLANG_VERSION=22
# The apt.llvm.org suite that matches the base. It is a separate ARG because the
# archive names its components `<suite>-<major>` and both halves are pinned.
ARG LLVM_APT_SUITE=trixie

# `apt-get update` reaches apt.llvm.org over TLS, and the slim base ships no CA
# bundle (every Debian mirror in its sources is http://). Without this the index
# download fails with `SSL routines::certificate verify failed` and the pinned
# packages are then simply "unable to locate" — measured on the first build of
# this recipe.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# The key verified in the fetch stage, and the one archive this image reads
# clang from. No `[trusted=yes]`, no `Acquire::AllowInsecureRepositories`.
COPY --from=fetch /staging/apt.llvm.org.asc /etc/apt/trusted.gpg.d/apt.llvm.org.asc
RUN set -eux; \
    echo "deb https://apt.llvm.org/${LLVM_APT_SUITE}/ llvm-toolchain-${LLVM_APT_SUITE}-${CLANG_VERSION} main" \
      > /etc/apt/sources.list.d/llvm.list

LABEL org.opencontainers.image.title="cppgo-ci" \
      org.opencontainers.image.description="Pinned Go + C++17 + libclang + golangci-lint + Hugo toolchain for cppgo" \
      org.opencontainers.image.source="https://github.com/Manu343726/cppgo" \
      org.opencontainers.image.licenses="MIT"

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

# Package-by-package reasons (all of these are load-bearing, none are padding):
#   ca-certificates  Go module downloads over HTTPS
#   curl, xz-utils   required at runtime by the workflows/scripts
#   git              `git describe` in tests, module VCS fallback
#   make             the Makefile is how CI drives the suite, and examples/*
#                    are built through their own Makefiles
#   nodejs           the interpreter `make prose-check` runs the vendored technical-
#                    prose checker in (`.agents/skills/technical-prose-style/scripts/
#                    prose-check.mjs` and `scripts/prose-check-godoc.mjs`, both ESM
#                    with Node built-ins and no npm tree). trixie's package is Node
#                    20.19.2 — a distro pin, like g++ and make, and the build asserts
#                    the major, so a base that loses node fails the image instead of
#                    the prose gate. `scripts/prose-check.sh` also needs `perl`,
#                    which the base already carries (5.40.1, perl-base).
#   pkg-config       asked for by the image contract; some cgo paths use it
#   cmake            the build system of `examples/full`, which drives `cmake -S`
#                    and `cmake --build` from its own Makefile. Without it the
#                    examples gate stops at `cmake: command not found` (measured
#                    in Forgejo run #778 on PR #61) — the same class of missing
#                    tool this image exists to prevent. Its runtime dependencies
#                    (libarchive, libcurl, jsoncpp, librhash) are what the trixie
#                    package pulls in; nothing else is added beside it, and no
#                    version is pinned because the gate asserts the configure, not
#                    a major. The CMake project itself is the example's, not this
#                    image's: this only provides the command it invokes.
#   g++              C++17 fixtures (internal/testlib, examples, build-examples)
#                    and the race detector's cgo; `cc` also builds the fake
#                    libclang the cxx tests use
#   clang-$CLANG_VERSION
#                    the compiler the project introspects with. clang 14, 19,
#                    20 and 22 differ in observable behaviour (see the base
#                    note), so the exact version is a tested dependency, not an
#                    incidental runtime.
#   libclang-$CLANG_VERSION-dev
#                    the C API internal/cxx dlopen()s, including
#                    clang_getOffsetOfBase, which libclang 19 does not export,
#                    plus the /usr/lib/llvm-20/lib/libclang.so.1 the loader
#                    reaches through the symlink created below.
#                    It also installs clang's static archives (libclangSema.a
#                    and friends, ~250 MB) which nothing here links at run
#                    time, so they are deleted below; the shared object, the
#                    resource headers and the C API headers stay.
#   binutils         `nm`, which internal/cxx uses as ground truth for
#                    out-of-line bodies. Its absence makes those tests SKIP and
#                    the coverage gate fail — exactly the class of bug this
#                    image exists to prevent, so it is explicit, not implicit.
#   libeigen3-dev,   the target libraries #15/#132's tests bind: Eigen's
#   libopencv-*      headers, OpenCV's headless module components (not the GUI
#                    or codec chain — see the header), and ArrayFire, which is
#                    copied in from the stage above because trixie has no
#                    package for it.
#   libfftw3-*3,     ArrayFire's *runtime*: the CPU backend links FFTW, LAPACKE
#   liblapacke,      and OpenBLAS in the builder stage, so those shared objects
#   libopenblas0     must exist here too. Without them `dlopen("libafcpu.so.3")`
#                    fails on unresolved dependencies and every af_* call answers
#                    `ArrayFire couldn't locate any backends` — which reads like
#                    a broken build and is a missing package (measured).
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash \
      binutils \
      ca-certificates \
      clang-${CLANG_VERSION} \
      cmake \
      curl \
      g++ \
      git \
      libclang-${CLANG_VERSION}-dev \
      libeigen3-dev \
      libfftw3-double3 \
      libfftw3-single3 \
      liblapacke \
      libopenblas0 \
      libopencv-calib3d-dev \
      libopencv-core-dev \
      libopencv-dnn-dev \
      libopencv-features2d-dev \
      libopencv-flann-dev \
      libopencv-imgcodecs-dev \
      libopencv-imgproc-dev \
      libopencv-ml-dev \
      libopencv-objdetect-dev \
      libopencv-photo-dev \
      libopencv-stitching-dev \
      libopencv-video-dev \
      make \
      nodejs \
      pkg-config \
      xz-utils \
 && rm -rf \
      /var/lib/apt/lists/* \
      /var/cache/apt/* \
      /usr/share/doc/* \
      /usr/share/man/* \
      /usr/share/info/* \
 && find /usr/lib/llvm-*/lib -maxdepth 1 -name '*.a' -delete

# ArrayFire, CPU backend, built in the `arrayfire` stage and staged under
# DESTDIR: `/staging/arrayfire/usr/local/{lib,include,share}` is exactly the
# layout /usr/local has, and libaf's compiled-in prefix is /usr/local, so the
# unified dispatcher finds libafcpu where it expects it.
COPY --from=arrayfire /staging/arrayfire/usr/local/ /usr/local/
RUN ldconfig

COPY --from=fetch /staging/rootfs/usr/local/go /usr/local/go
COPY --from=fetch /staging/rootfs/usr/local/bin/golangci-lint /usr/local/bin/golangci-lint
COPY --from=fetch /staging/rootfs/usr/local/bin/hugo /usr/local/bin/hugo

# Drop what a CI toolchain never reads: Go's conformance tests, the API index,
# the documentation and the editor/tool misc tree. The compiler, linker, stdlib
# and pkg/tool stay — they are what `go test -race` actually invokes.
RUN rm -rf /usr/local/go/test /usr/local/go/api /usr/local/go/doc /usr/local/go/misc

# libclang discovery: internal/cxx looks for $CPPGO_LIBCLANG, then
# /usr/lib/libclang.so, then /usr/lib/libclang-*.so*, then /usr/lib/llvm-*/lib/.
# apt.llvm.org's package puts the real object in the multiarch directory
# (/usr/lib/x86_64-linux-gnu/libclang-20.so.20) and a versioned hop beside it
# (/usr/lib/llvm-20/lib/libclang-20.so.1), which the loader's last pattern does
# reach — but the unversioned name it tries first (/usr/lib/libclang.so) is what
# the environment actually resolves, so it is created here and then proven.
# It is then proven with a real dlopen + dlsym that the symlink resolves, that
# the C API is exported, that the major matches the pin **and that the version's
# own symbols are there**: `clang_getOffsetOfBase` is the one internal/cxx calls
# that a major can be too old for (clang 19 does not export it, clang 20 does),
# and the failure it causes in the suite is an `undefined symbol` after a
# successful load — far away from the pin that caused it. A build-time failure
# is cheaper than a silent test skip that only shows up as a red coverage gate.
#
# The versionless compiler names are what a `clang` metapackage provides; the
# pinned clang-20 package does not create them, so they are created here.
RUN <<'SCRIPT'
set -eux

ln -sfn "/usr/bin/clang-${CLANG_VERSION}" /usr/bin/clang
ln -sfn "/usr/bin/clang++-${CLANG_VERSION}" /usr/bin/clang++

libclang="$(ls -1 /usr/lib/llvm-*/lib/libclang*.so.1 2>/dev/null | head -n 1 || true)"
if [ -z "$libclang" ]; then
  libclang="$(ls -1 /usr/lib/llvm-*/lib/libclang.so 2>/dev/null | head -n 1 || true)"
fi
[ -n "$libclang" ] || { echo "no libclang shared object found; libclang-${CLANG_VERSION}-dev is missing" >&2; exit 1; }
ln -sfn "$libclang" /usr/lib/libclang.so
ldconfig

cat >/tmp/libclang-probe.c <<'PROBE'
#include <dlfcn.h>
#include <stdio.h>

/* libclang's CXString: { const void *data; unsigned private_flags; }. */
typedef struct { const void *data; unsigned private_flags; } CXString;

int main(int argc, char **argv) {
    void *handle = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (!handle) {
        fprintf(stderr, "dlopen(%s): %s\n", argv[1], dlerror());
        return 1;
    }
    CXString (*get_version)(void) = (CXString (*)(void))dlsym(handle, "clang_getClangVersion");
    const char *(*get_string)(CXString) = (const char *(*)(CXString))dlsym(handle, "clang_getCString");
    if (!get_version || !get_string) {
        fprintf(stderr, "libclang C API symbols not found\n");
        return 2;
    }
    /* The version-dependent symbol internal/cxx binds: absent before LLVM 20. */
    if (!dlsym(handle, "clang_getOffsetOfBase")) {
        fprintf(stderr, "clang_getOffsetOfBase is not exported by %s\n", argv[1]);
        return 3;
    }
    printf("%s\n", get_string(get_version()));
    return 0;
}
PROBE

gcc -std=c11 -O1 -o /tmp/libclang-probe /tmp/libclang-probe.c -ldl
/tmp/libclang-probe /usr/lib/libclang.so | tee /tmp/libclang-version
grep -q "clang version ${CLANG_VERSION}\." /tmp/libclang-version
rm -f /tmp/libclang-probe /tmp/libclang-probe.c /tmp/libclang-version
SCRIPT

# git over a bind mount owned by the host user: without this, `git describe` and
# friends abort with "dubious ownership" when the container runs as root.
RUN git config --system --add safe.directory '*'

# GOTOOLCHAIN=local: never download a second Go at test time. If go.mod moves
# past GO_VERSION the build of the module fails with a clear message, and the
# fix is to bump the ARG above — the same discipline as every other pin here.
ENV PATH="/usr/local/go/bin:${PATH}" \
    GOTOOLCHAIN=local \
    LANG=C.UTF-8

# The target libraries, proven by a real compile, a real link and a real run
# rather than by a package listing: Eigen is header-only so its check is the
# compile and the trace, OpenCV's is a `cv::Mat` through `cvtColor`, and
# ArrayFire's is a reduction through the CPU backend plus the version the
# library reports. A library that is present but broken fails here, at build
# time, and not as an `undefined symbol` in a test months later.
RUN <<'SCRIPT'
set -eux

mkdir -p /tmp/targets-check
cat >/tmp/targets-check/check.cpp <<'CHECK'
#include <cmath>
#include <cstdio>

#include <Eigen/Dense>
#include <af/algorithm.h>
#include <af/array.h>
#include <af/data.h>
#include <af/util.h>

#include <opencv2/calib3d.hpp>
#include <opencv2/core.hpp>
#include <opencv2/core/version.hpp>
#include <opencv2/dnn.hpp>
#include <opencv2/features2d.hpp>
#include <opencv2/flann.hpp>
#include <opencv2/imgproc.hpp>
#include <opencv2/ml.hpp>
#include <opencv2/objdetect.hpp>
#include <opencv2/photo.hpp>
#include <opencv2/stitching.hpp>
#include <opencv2/video/background_segm.hpp>

/* The libraries this image carries, each proven by a call and a value it
 * returns rather than by a package listing: Eigen by a trace, ArrayFire by a
 * reduction through the CPU backend, and every OpenCV module issue #132 binds
 * by one call. A module that is installed but broken fails the build here, at
 * build time, instead of as an `undefined symbol` in a test months later.
 *
 * Two of these deliberately prove the error path of a file-driven API: there is
 * no cascade and no ONNX model in this image, so `CascadeClassifier` is empty
 * and `dnn::Net` is empty. The examples ship the smallest real fixture they
 * need; this probe proves the module links and runs. */
int main() {
    const Eigen::Matrix3d m = Eigen::Matrix3d::Identity();
    const double trace = m.trace();

    cv::Mat bgr(16, 16, CV_8UC3, cv::Scalar(0, 0, 255));
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    const int g = gray.at<uchar>(0, 0);

    af::array a = af::constant(1.5, 3);
    const double total = af::sum<double>(a);

    int major = 0, minor = 0, patch = 0;
    af_get_version(&major, &minor, &patch);

    /* video: a background model, which is the module's entry point. */
    cv::Ptr<cv::BackgroundSubtractor> mog = cv::createBackgroundSubtractorMOG2();
    cv::Mat mask;
    mog->apply(bgr, mask, 0.5);
    const int foreground = cv::countNonZero(mask);

    /* calib3d: the rotation-vector conversion, the cheapest exact answer. */
    cv::Mat rvec, jacobian;
    cv::Rodrigues(cv::Mat::eye(3, 3, CV_64F), rvec, jacobian);
    const double rvecNorm = cv::norm(rvec);

    /* features2d: ORB over a deterministic texture, and the descriptor width
     * proves the module computed descriptors and not just keypoints. */
    cv::Mat field(128, 128, CV_8UC1);
    unsigned seed = 12345u;
    for (int y = 0; y < field.rows; y++) {
        for (int x = 0; x < field.cols; x++) {
            seed = seed * 1664525u + 1013904223u;
            field.at<uchar>(y, x) = uchar(seed >> 24);
        }
    }
    cv::Ptr<cv::ORB> orb = cv::ORB::create(500);
    std::vector<cv::KeyPoint> keys;
    cv::Mat descriptors;
    orb->detectAndCompute(field, cv::noArray(), keys, descriptors);
    const int keypointCount = int(keys.size());

    /* objdetect: no cascade file ships in the image. */
    cv::CascadeClassifier cascade;
    const int cascadeEmpty = int(cascade.empty());

    /* photo: denoising leaves a constant field where it was. */
    cv::Mat denoised;
    cv::fastNlMeansDenoising(gray, denoised, 3.0f, 3, 7);
    const int denoisedValue = denoised.at<uchar>(0, 0);

    /* flann: an index parameter object and the value it carries. */
    cv::flann::KDTreeIndexParams trees(4);
    const int treeCount = trees.getInt("trees", 0);

    /* ml and dnn: a model object of each, with the state it reports. */
    cv::Ptr<cv::ml::SVM> svm = cv::ml::SVM::create();
    const int svmType = svm->getType();
    cv::dnn::Net net;
    const int netEmpty = int(net.empty());

    /* stitching: the pipeline object, which is the module's entry point. */
    cv::Ptr<cv::Stitcher> stitcher = cv::Stitcher::create();

    std::printf("eigen=%d.%d.%d trace=%.0f opencv=%s gray=%d arrayfire=%d.%d.%d sum=%.1f "
                "fg=%d rvec=%.1f keypoints=%d descriptors=%d cascade_empty=%d denoised=%d "
                "flann_trees=%d svm_type=%d net_empty=%d stitcher=%d\n",
                EIGEN_WORLD_VERSION, EIGEN_MAJOR_VERSION, EIGEN_MINOR_VERSION, trace,
                CV_VERSION, g, major, minor, patch, total,
                foreground, rvecNorm, keypointCount, descriptors.cols, cascadeEmpty,
                denoisedValue, treeCount, svmType, netEmpty, int(stitcher != nullptr));

    if (std::fabs(trace - 3.0) > 1e-9) return 1;
    if (g != 76) return 2;            /* BGR(0,0,255) is red: its grey is 76 */
    if (std::fabs(total - 4.5) > 1e-9) return 3;
    if (major == 0) return 4;
    if (foreground != 256) return 5;  /* the first frame of a 16x16 is foreground */
    if (rvecNorm > 1e-9) return 6;
    if (keypointCount == 0) return 7;
    if (descriptors.rows != keypointCount || descriptors.cols != 32) return 8;
    if (cascadeEmpty == 0) return 9;
    if (denoisedValue != g) return 10;
    if (treeCount != 4) return 11;
    if (svmType != cv::ml::SVM::C_SVC) return 12;
    if (netEmpty == 0) return 13;
    if (stitcher == nullptr) return 14;
    return 0;
}
CHECK

g++ -std=c++17 -O2 -o /tmp/targets-check/check /tmp/targets-check/check.cpp \
    -I/usr/include/eigen3 -I/usr/include/opencv4 \
    -lopencv_core -lopencv_imgproc -lopencv_imgcodecs \
    -lopencv_video -lopencv_calib3d -lopencv_features2d \
    -lopencv_objdetect -lopencv_photo -lopencv_flann \
    -lopencv_ml -lopencv_dnn -lopencv_stitching \
    -laf

ldconfig -p | grep -E 'libaf(cpu)?\.so|libspdlog\.so' || {
  echo "the loader does not know /usr/local/lib libaf; the ld cache was not refreshed" >&2
  exit 1
}
# The backend is loaded with dlopen at run time, so a missing runtime dependency
# is invisible to the link above and only shows up as "couldn't locate any
# backends". This is that check, named.
if ldd /usr/local/lib/libafcpu.so.3 | grep -q 'not found'; then
  ldd /usr/local/lib/libafcpu.so.3 >&2
  echo "libafcpu has an unresolved dependency; the final image is missing a runtime library" >&2
  exit 1
fi
AF_SHOW_LOAD_PATH=1 /tmp/targets-check/check

# The headers and the ABI files the link above does not exercise.
test -f /usr/include/eigen3/Eigen/Dense
test -f /usr/include/opencv4/opencv2/imgproc.hpp
test -f /usr/local/include/af/array.h
ldconfig -p | grep -q 'libafcpu\.so'
rm -rf /tmp/targets-check
SCRIPT

# Fail the build, not the job, if a pinned tool is missing or wrong.
RUN set -eux; \
    go version; \
    node --version; \
    node --version | grep -qE '^v(2[0-9]|[3-9][0-9])\.' || { echo "node is older than 20" >&2; exit 1; }; \
    golangci-lint --version; \
    hugo version; \
    test -x /usr/bin/nm; \
    g++ --version | head -n 1; \
    clang --version | head -n 1; \
    cmake --version | head -n 1; \
    hugo version | grep -qi extended; \
    go version | grep -q "go1\.27\.3 "; \
    clang --version | grep -q "clang version ${CLANG_VERSION}\."

CMD ["bash"]
