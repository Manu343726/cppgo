#!/usr/bin/env bash
#
# run.sh — run the whole comparison matrix and regenerate README.md's table.
#
# Every arm is one `go test -run '^$' -bench . -benchmem -count=N` invocation,
# saved verbatim under results/ and then rendered by cmd/benchtable. Nothing in
# the README is typed by hand: the table and the environment block are both
# rewritten from these files, and a re-run with the same tree overwrites both.
#
#   BENCH_COUNT    -count for each arm (default 6: the table shows the median
#                  and the spread of those runs)
#   BENCH_TIME     -benchtime, empty for the go test default (1s per benchmark)
#   BENCH_TIMEOUT  -timeout (default 30m)
#
# Environment: the cgo arm is the only one that needs cgo. gobase and cppgo run
# with CGO_ENABLED=0, which is the project's own contract; the README states
# the mode each file was produced under.

set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/.." && pwd)"
cd "$ROOT"

GO="${GO:-go}"
CXX="${CXX:-g++}"
COUNT="${BENCH_COUNT:-6}"
BENCHTIME="${BENCH_TIME:-}"
TIMEOUT="${BENCH_TIMEOUT:-30m}"

if [[ ! -f build/libbench.so ]]; then
  echo "run.sh: build/libbench.so is missing — run 'make gen' first" >&2
  exit 1
fi

flags=(-run '^$' -bench . -benchmem -count="$COUNT" -timeout="$TIMEOUT")
if [[ -n $BENCHTIME ]]; then
  flags+=(-benchtime="$BENCHTIME")
fi

mkdir -p results

# The environment block is captured before the first run writes anything: the
# `Tree:` line has to describe the tree the measurements were taken from, and
# the run's own output files are tracked, so writing them first would report a
# dirty tree that only exists because the run started.
#
# These are facts read from the machine at the moment of the run, not a
# remembered description of it; anything that cannot be read is reported as
# missing rather than guessed.
read_or_missing() {
  local path="$1"
  if [[ -r $path ]]; then cat "$path"; else echo "missing"; fi
}

cpu_model="$(awk -F': ' '/^model name/ { print $2; exit }' /proc/cpuinfo 2>/dev/null || true)"
[[ -n ${cpu_model:-} ]] || cpu_model="unknown"
logical_cpus="$(nproc 2>/dev/null || echo unknown)"
governor="$(read_or_missing /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)"
cur_freq="$(read_or_missing /sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq)"
max_freq="$(read_or_missing /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq)"
toolchain="$(go version)"
compiler="$("$CXX" --version | head -n 1)"

{
  printf 'CPU: %s, %s logical CPUs\n' "$cpu_model" "$logical_cpus"
  printf 'Kernel: %s %s\n' "$(uname -sr)" "$(uname -m)"
  printf 'Go: %s\n' "$toolchain"
  printf 'C++ compiler: %s\n' "$compiler"
  printf 'cgo: gobase and cppgo run with CGO_ENABLED=0, cgo runs with CGO_ENABLED=1\n'
  printf 'Frequency: governor %s; cpu0 at %s kHz (cpuinfo max %s kHz) when the run started\n' \
    "$governor" "$cur_freq" "$max_freq"
  printf 'Tuning: none — no pinning, no governor change, no boost change, default GOMAXPROCS\n'
  printf 'Runs: %s per arm; each cell is the median of those runs, the range is their min–max\n' "$COUNT"
  printf 'Command: go test -run %s -bench . -benchmem -count=%s -timeout=%s\n' "'^\$'" "$COUNT" "$TIMEOUT"
  printf 'Tree: %s\n' "$(git rev-parse --short HEAD 2>/dev/null || echo 'not a git checkout')$(git diff --quiet 2>/dev/null || echo ' (dirty)')"
  printf 'Date: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > results/environment.txt

# run <label> <cgo-mode> <package>...
run() {
  local label="$1" mode="$2"
  shift 2
  echo "== $label: CGO_ENABLED=$mode go test ${flags[*]} $* =="
  CGO_ENABLED="$mode" "$GO" test "${flags[@]}" "$@" | tee "results/$label.txt"
}

run gobase 0 ./gobase
run cppgo 0 ./cppgo
run cgo 1 ./cgo

CGO_ENABLED=0 "$GO" run ./cmd/benchtable -write README.md -env results/environment.txt \
  gobase=results/gobase.txt cppgo=results/cppgo.txt cgo=results/cgo.txt

echo
echo "README.md: table and environment regenerated from results/"
