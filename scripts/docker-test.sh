#!/usr/bin/env bash
#
# docker-test.sh — build the CI image and run the coverage suite inside it.
#
# The point of the image is that internal/cxx can reach libclang and stop
# skipping itself, so this runs the same commands ci.yml runs, from a bind mount
# of the repository (nothing is baked into the image), with
# CPPGO_REQUIRE_LIBCLANG=1 so a missing libclang fails instead of skipping.
#
# Usage:
#   scripts/docker-test.sh                 # build, then run the suite
#   IMAGE=cppgo-ci:local scripts/docker-test.sh
#
# Exit codes: 0 = the suite passed inside the image, non-zero = it did not.

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${IMAGE:-cppgo-ci:local}"

echo "== docker build -t ${image} ${repo_dir}"
docker build -t "$image" "$repo_dir"

echo "== suite inside ${image}"
docker run --rm \
  -e CPPGO_REQUIRE_LIBCLANG=1 \
  -v "${repo_dir}:/src" \
  -w /src \
  "$image" \
  sh -c '
    set -e
    go vet ./...
    CGO_ENABLED=0 go test -count=1 ./...
    CGO_ENABLED=1 go test -race -count=1 -covermode=atomic \
      -coverprofile=/tmp/c.out ./...
    ./scripts/coverage-gate.sh /tmp/c.out
  '
