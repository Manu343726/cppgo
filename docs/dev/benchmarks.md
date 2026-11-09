# Benchmarks

The manual has a [Performance page](../../site/content/explanation/benchmarks.md)
that publishes the measured comparison. The suite itself lives in
[`benchmarks/`](../../benchmarks/README.md): its README documents every arm and
carries the generated tables, and [`results/`](../../benchmarks/results/) is the
committed `go test` output they are built from.

This page is the methodology behind the manual page, plus the state of the work:
what the numbers mean for the code, and what is known to be wrong with it.

## Method

1. **One library, one set of symbols.** [`src/bench.cpp`](../../benchmarks/src/bench.cpp)
   is compiled once into `build/libbench.so` with `-std=c++17 -O2 -fPIC` and its
   own soname. Both FFI arms link against that same object; neither compiles the
   work itself.
2. **Every arm computes the same values.**
   [`internal/benchdata`](../../benchmarks/internal/benchdata/benchdata.go) is the
   plain-Go reference, and each FFI arm's tests compare its answers against it.
3. **Each arm is its own process**, with the cgo mode it needs, so the cgo
   runtime cannot inflate a measurement that does not use it.
4. **`-benchmem` and `-count=6` on every arm.** The table shows the median ns/op,
   the min–max of the six runs, and the bytes and allocations per operation.
   Nothing is averaged away.
5. **No tuning.** Default `GOMAXPROCS`; no pinning, no governor or boost change.
   Frequency scaling is on, so absolute nanoseconds move with the machine and the
   ratios are the reproducible claim.
6. **The table is generated.** [`scripts/run.sh`](../../benchmarks/scripts/run.sh)
   writes `results/*.txt` and [`cmd/benchtable`](../../benchmarks/cmd/benchtable/main.go)
   rewrites the generated block in
   [`benchmarks/README.md`](../../benchmarks/README.md). No number in that file
   is typed by hand.

## Environment of the published run

Verbatim from [`results/environment.txt`](../../benchmarks/results/environment.txt):

```
CPU: AMD Ryzen 7 6800HS with Radeon Graphics, 16 logical CPUs
Kernel: Linux 6.12.108-1-MANJARO x86_64
Go: go version go1.27.0-X:nodwarf5 linux/amd64
C++ compiler: g++ (GCC) 16.2.1 20260810
cgo: gobase and cppgo run with CGO_ENABLED=0, cgo runs with CGO_ENABLED=1
Frequency: governor powersave; cpu0 at 964830 kHz (cpuinfo max 3201000 kHz) when the run started
Tuning: none — no pinning, no governor change, no boost change, default GOMAXPROCS
Runs: 6 per arm; each cell is the median of those runs, the range is their min–max
Command: go test -run '^$' -bench . -benchmem -count=6 -timeout=30m
Tree: 5171d1b
Date: 2026-11-09T00:13:39Z
```

The manual page keeps the neutral half of that block (CPU, OS, Go, compiler,
frequency state, tuning, run count) and drops the command, the tree and the date
as provenance.

## Regenerating

```console
$ make benchmarks                  # from the repository root: runs every arm
$ make -C benchmarks bench         # the same, from the suite's directory
$ make -C benchmarks table         # re-render benchmarks/README.md from results/
```

`make -C benchmarks table` does not run anything: it re-renders the generated
block from the committed `results/*.txt`. Use it after reviewing a results
change, and use the full run after a change that could move a number.

A single arm by hand:

```console
# plain Go baseline and the cppgo arm: no cgo anywhere
$ CGO_ENABLED=0 go test -run '^$' -bench . -benchmem -count=6 ./gobase ./cppgo

# the cgo arm: needs cgo, and the library built first
$ CGO_ENABLED=1 go test -run '^$' -bench . -benchmem -count=6 ./cgo
```

The full run wants an idle machine. The suite is not part of the CI gate
(`make benchmarks` is explicit about that): a shared runner cannot produce a
number worth publishing.

## How the manual page is derived

- Every ns/op value on the Performance page is the median in the generated table
  of [`benchmarks/README.md`](../../benchmarks/README.md), which comes from
  `results/*.txt`; nothing is typed by hand.
- The "cppgo vs cgo" column is the two medians divided, rounded to one decimal.
  The suite's own `vs go_direct` column is the equivalent against the plain-Go
  arm.
- The prose claims are limited to what those two tables show. If a run changes a
  number, update the page from the new table in the same commit.

## Caveats

- Frequency scaling is on. The absolute numbers are not comparable across
  machines or clock states; the ratios are the claim.
- The cgo arm is the fastest correct wrapper for this work — borrowed Go memory
  as `(pointer, length)`, results copied out once. It is not a `CString`-based
  wrapper, which would pay an allocation and a copy per string.
- The cppgo numbers are the generated API as shipped, entry point included, not
  the bare trampoline. [`DESIGN.md`](../../DESIGN.md) prices the trampoline alone
  at 15.9 ns; the
  difference between that figure and `BenchmarkCall/cppgo_direct` is the
  generated entry point around it.
- Error and callback rows measure the whole Go-visible operation, including the
  error value each API constructs. The three arms deliberately build different
  error types, so those rows are what a caller pays, not a byte-identical
  computation.

## Defects the suite found ([#51](https://git.manu343726.com/manu343726/cppgo/issues/51))

The suite is the oracle for the performance work. Each item below is visible in
the table and is a code defect, not a measurement artefact:

1. **A symbol lookup per failure, and per `Close`.** `LastError` calls
   `Library.Symbol`, which calls `dlsym` uncached. `BenchmarkStatusFailure` is
   tens of times `BenchmarkStatusOK` with only the status changed, and the
   generated `Close` always calls `cppgoCloseError`, so
   `BenchmarkObjectNewClose` is far slower than the cgo arm (2974 ns against
   467 ns, 12 allocations against 1). Fix: a per-`Library` symbol cache with
   correct `Close` semantics — runtime only. The suite is re-run after it, and
   the manual page is updated from the new medians.
2. **Generated entry-point overhead.** Every call runs `cppgoOpen` (a mutex) and
   `ProbeStack` before the trampoline; measured separately on this machine, the
   lock and the probe cost about as much as the `asm.Call` they guard. That is
   why `cppgo_direct` is more than twice the 15.9 ns trampoline. Emitter work.
3. **By-value POD arguments escape.** The generated code takes the address of
   every by-value parameter and also passes it to `KeepAlive`; `go build
   -gcflags=-m` reports `moved to heap: a`, `moved to heap: b` and `moved to
   heap: cppgoResult` for `Midpoint`. Three allocations per call against cgo's
   one. Emitter work.
4. **The reentrant binding registers per call.** `RegisterCallback` copies the
   64-slot table under its lock before every call, and `ReleaseCallback` after
   it: 1200 B and 6 allocations per call, ~5× cgo's callback. A registration at
   binding time is what the row asks for. Emitter work.

## Related

- The suite's own documentation, arm by arm: [`benchmarks/README.md`](../../benchmarks/README.md).
- The manual page it backs: [Performance](../../site/content/explanation/benchmarks.md).
