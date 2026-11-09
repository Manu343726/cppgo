#!/usr/bin/env bash
#
# test-targets.sh — run the test target of every compatibility-target example.
#
# Issue #42 lands one example module per target library (Eigen, OpenCV,
# ArrayFire) under examples/. This script is the explicit gate that names those
# three libraries and runs each one's own test target, so the target evidence is
# exercised by CI instead of being local-only. It does not build C++ itself: an
# example owns its build, and its Makefile's `test` target depends on `gen`, so
# the library, the binding and the tests are produced by the example.
#
# It is deliberately decoupled from #42 landing: a library whose directory or
# Makefile does not exist yet is reported and skipped, not failed. The pinned CI
# image carries all three libraries, so in CI a skip means the example has not
# landed — the job log names each one it ran and each one it skipped.
#
# Note that scripts/build-examples.sh already globs examples/*/ and runs `make`
# in every directory that has one, so these modules are also built by the `test`
# job once they land. This script is not a duplicate of that: it names the three
# libraries the epic cares about and reports them by name, including the ones
# that are still absent.
#
# Usage:
#   scripts/test-targets.sh [examples-dir]      # defaults to examples
#
# Environment: MAKE (default make).
#
# Exit codes: 0 = every present example passed, 1 = an example's test failed.

set -euo pipefail

MAKE="${MAKE:-make}"
EXAMPLES_DIR="${1:-examples}"

# The three compatibility targets of epic #1, in the order the report names them.
TARGETS=(opencv arrayfire eigen)

if [[ ! -d $EXAMPLES_DIR ]]; then
  echo "no $EXAMPLES_DIR/ directory — nothing to test."
  exit 0
fi

tested=0
skipped=0
ran=()

for lib in "${TARGETS[@]}"; do
  dir="$EXAMPLES_DIR/$lib"

  if [[ ! -d $dir ]]; then
    echo "-- $lib: no $dir/ — skipped (the example has not landed)"
    skipped=$((skipped + 1))
    continue
  fi

  if [[ ! -f "$dir/Makefile" && ! -f "$dir/makefile" ]]; then
    echo "-- $lib: $dir/ has no Makefile — skipped (an example owns its build)"
    skipped=$((skipped + 1))
    continue
  fi

  echo "== $lib: $MAKE -C $dir test"
  "$MAKE" -C "$dir" test
  tested=$((tested + 1))
  ran+=("$lib")
done

echo
if (( tested > 0 )); then
  echo "targets: $tested tested (${ran[*]}), $skipped skipped"
else
  echo "targets: 0 tested, $skipped skipped — no target example has landed yet"
fi
