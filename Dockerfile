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
# is newer than 14. The same tree with trixie's clang 19 passes at 99.4% total
# coverage; clang 19 is therefore the oldest version that satisfies the code,
# and trixie is the Debian stable that carries it. g++ 14 covers the C++17
# fixtures. arm64 maps to the same package names, so the build stays portable.
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

# Go must match the `go` directive in go.mod (1.25.0). Bump both together: the
# final image sets GOTOOLCHAIN=local, so a go.mod newer than this fails loudly
# instead of silently downloading a second toolchain.
ARG GO_VERSION=1.25.0
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

# ---------------------------------------------------------------------------
# Stage 2 — the image CI runs in.
# ---------------------------------------------------------------------------
FROM debian:${DEBIAN_VERSION}-slim

# Pinned libclang/libclang major. Trixie's metapackages (`clang`,
# `libclang-dev`) resolve to these, but naming the version keeps the image
# honest if the base ever floats: changing it is a code decision (see the base
# note above), not an image tweak.
ARG CLANG_VERSION=19

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
#                    the compiler the project introspects with. clang 14 vs 19
#                    differ in observable behaviour (see the base note), so the
#                    exact version is a tested dependency, not an incidental
#                    runtime.
#   libclang-$CLANG_VERSION-dev
#                    the C API internal/cxx dlopen()s, plus the unversioned
#                    /usr/lib/llvm-19/lib/libclang.so the loader looks for.
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
# Debian's package puts the real object in the multiarch directory with the
# version *in the middle* (libclang-19.so.1), a name the last pattern does not
# match, so the unversioned name the loader tries first must be created here.
# It is then proven with a real dlopen + dlsym that the symlink resolves, that
# the C API is exported and that the major matches the pin — a build-time
# failure is far cheaper than a silent test skip that only shows up as a red
# coverage gate.
#
# The versionless compiler names are what Debian's `clang` metapackage provides;
# we install the pinned clang-19 instead (the metapackage is only a symlink
# carrier), so create them the same way.
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
    go version | grep -q "go1\.25\.0 "; \
    clang --version | grep -q "clang version ${CLANG_VERSION}\."

CMD ["bash"]
