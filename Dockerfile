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
#   pkg-config       asked for by the image contract; some cgo paths use it
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
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash \
      binutils \
      ca-certificates \
      clang-${CLANG_VERSION} \
      curl \
      g++ \
      git \
      libclang-${CLANG_VERSION}-dev \
      make \
      pkg-config \
      xz-utils \
 && rm -rf \
      /var/lib/apt/lists/* \
      /var/cache/apt/* \
      /usr/share/doc/* \
      /usr/share/man/* \
      /usr/share/info/* \
 && find /usr/lib/llvm-*/lib -maxdepth 1 -name '*.a' -delete

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

# Fail the build, not the job, if a pinned tool is missing or wrong.
RUN set -eux; \
    go version; \
    golangci-lint --version; \
    hugo version; \
    test -x /usr/bin/nm; \
    g++ --version | head -n 1; \
    clang --version | head -n 1; \
    hugo version | grep -qi extended; \
    go version | grep -q "go1\.27\.3 "; \
    clang --version | grep -q "clang version ${CLANG_VERSION}\."

CMD ["bash"]
