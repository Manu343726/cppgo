#!/usr/bin/env bash
#
# build-examples.sh — compile every C++ fixture under examples/.
#
# cppgo itself never compiles C++: the fixtures are built here only so the
# tests (`make test`, CI) can dlopen a real shared library.
#
# A fixture directory (examples/<name>/) is built with the first of:
#   Makefile        -> make -C <dir>
#   CMakeLists.txt  -> cmake -S <dir> -B <dir>/build && cmake --build
#   *.cpp / *.cc    -> $CXX -std=c++17 -O2 -fPIC -shared -> <dir>/lib<name>.so
#
# Directories with none of the above are reported and skipped; examples/ being
# absent or empty is not an error (the tree may be incomplete).
#
# Environment: CXX (default g++), CXXFLAGS (extra flags for the plain-.cpp path).

set -euo pipefail

CXX="${CXX:-g++}"
CXXFLAGS="${CXXFLAGS:-}"
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
    make -C "$dir"
    built=$((built + 1))
    continue
  fi

  if [[ -f "$dir/CMakeLists.txt" ]]; then
    echo "== $name: cmake"
    cmake -S "$dir" -B "$dir/build" -DCMAKE_BUILD_TYPE=Release
    cmake --build "$dir/build" --parallel
    built=$((built + 1))
    continue
  fi

  sources=("$dir"/*.cpp "$dir"/*.cc)
  if [[ ${#sources[@]} -gt 0 ]]; then
    out="$dir/lib${name}.so"
    echo "== $name: $CXX -shared -> $(basename "$out")"
    # shellcheck disable=SC2086 # CXXFLAGS is intentionally word-split
    "$CXX" -std=c++17 -O2 -fPIC -shared -I"$dir" $CXXFLAGS \
      -o "$out" "${sources[@]}"
    built=$((built + 1))
    continue
  fi

  echo "-- $name: no Makefile, CMakeLists.txt or C++ sources — skipped"
  skipped=$((skipped + 1))
done

echo
echo "example fixtures: $built built, $skipped skipped"
