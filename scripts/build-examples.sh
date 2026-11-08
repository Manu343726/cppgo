#!/usr/bin/env bash
#
# build-examples.sh — build, generate and test every example under examples/.
#
# Each example owns its build. Its Makefile does the three things a v4 example
# has to do, in this order:
#
#   1. compiles the user's C++ library with the project's own flags and its own
#      soname;
#   2. runs `cppgo gen`, which reads the header, writes the Go API, the
#      assembly and the shim sources, and compiles the shim itself against that
#      library;
#   3. runs the example's tests and vet.
#
# This script only finds the examples and runs their default target. It never
# compiles C++ itself, which is what keeps the contract's line intact: no
# cppgo-generated translation unit can end up in a user's library.
#
# Directories without a Makefile are reported and skipped; examples/ being
# absent is not an error (the tree may be incomplete).
#
# Environment: MAKE (default make); CXX and CXXFLAGS reach the examples' own
# Makefiles through the environment, as they always did.

set -euo pipefail

MAKE="${MAKE:-make}"
EXAMPLES_DIR="${1:-examples}"

if [[ ! -d $EXAMPLES_DIR ]]; then
  echo "no $EXAMPLES_DIR/ directory — nothing to build."
  exit 0
fi

shopt -s nullglob
built=0
skipped=0

for dir in "$EXAMPLES_DIR"/*/; do
  name="$(basename "$dir")"

  if [[ -f "$dir/Makefile" || -f "$dir/makefile" ]]; then
    echo "== $name: make"
    "$MAKE" -C "$dir"
    built=$((built + 1))
    continue
  fi

  echo "-- $name: no Makefile — skipped (a v4 example owns its build there)"
  skipped=$((skipped + 1))
done

echo
echo "examples: $built built, $skipped skipped"
