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
Frequency: governor performance; cpu0 at 3602458 kHz (cpuinfo max 4785000 kHz) when the run started
Tuning: none — no pinning, no governor change, no boost change, default GOMAXPROCS
Runs: 6 per arm; each cell is the median of those runs, the range is their min–max
Command: go test -run '^$' -bench . -benchmem -count=6 -timeout=30m
Tree: 532703d
Date: 2026-11-09T10:51:23Z
```

The `Frequency:` line is read from the machine at the start of the run, and this
run was made with the laptop on its `performance` profile — 3602458 kHz at the
start, boost on, `cpuinfo_max_freq` at 4785000 kHz. The `Tuning:` line is
written by the script and describes what the harness does: no CPU pinning, no
governor or boost write per run, default `GOMAXPROCS`. It does not describe the
profile the machine was on, which is what the `Frequency:` line records.

The manual page keeps the neutral half of that block (CPU, OS, Go, compiler,
frequency state, `GOMAXPROCS`, run count) and drops the command, the tree, the
date and the `Tuning:` wording as provenance.

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

- The Performance page is organised per scenario, in the order the scenarios are
  measured, and each scenario carries its numbers under the code they were taken
  from: see
  [`documentation-rules.md` § Rule 5](../../.agents/skills/extra/documentation-rules.md).
- Every ns/op value on the page is the median in the generated table of
  [`benchmarks/README.md`](../../benchmarks/README.md), which comes from
  `results/*.txt`; nothing is typed by hand. The `Iterations` column is the
  `b.N` of the same lines, quoted as the lowest–highest of the six runs.
- Every ratio in the prose is two of those medians divided. Where two arms'
  min–max ranges overlap, the page says they are not distinguishable instead of
  quoting a ratio.
- The prose claims are limited to what the tables show and to what the code
  beside them says. If a run changes a number, update the page from the new
  table in the same commit.

## Caveats

- Frequency scaling is on. The absolute numbers are not comparable across
  machines or clock states; the ratios are the claim.
- The published run is made on the laptop's `performance` profile, so its
  nanoseconds are much smaller than a run at a `powersave` clock and cannot be
  compared with one: `BenchmarkCall/go_direct` is 1.50 ns here against 6.33 ns
  on the run at 964830 kHz that preceded it. Compare ratios, or re-run.
- The cgo arm is the fastest correct wrapper for this work — borrowed Go memory
  as `(pointer, length)`, results copied out once. It is not a `CString`-based
  wrapper, which would pay an allocation and a copy per string.
- The cppgo numbers are the generated API as shipped, entry point included, not
  the bare trampoline. [`DESIGN.md`](../../DESIGN.md) prices the trampoline alone
  at 15.9 ns, measured at 964 MHz: that figure and this run's 10.5 ns are not
  like-for-like, because the clocks differ, so subtracting one from the other
  does not give the entry point's share.
- Error and callback rows measure the whole Go-visible operation, including the
  error value each API constructs. The three arms deliberately build different
  error types, so those rows are what a caller pays, not a byte-identical
  computation.

## Defects the suite found ([#51](https://git.manu343726.com/manu343726/cppgo/issues/51))

The suite is the oracle for the performance work. Each item below is visible in
the table and is a code defect, not a measurement artefact:

1. **A symbol lookup per failure, and per `Close` — fixed.** `LastError` called
   `Library.Symbol`, which called `dlsym` uncached. `BenchmarkStatusFailure` was
   tens of times `BenchmarkStatusOK` with only the status changed, and the
   generated `Close` always called `cppgoCloseError`, so
   `BenchmarkObjectNewClose` was far slower than the cgo arm (2974 ns against
   467 ns, 12 allocations against 1). The fix is a per-`Library` symbol cache
   holding the address or the loader's error for every name asked for
   ([`cppgo.go`](../../cppgo.go), `Library.symbols`), runtime only. After the
   re-run the two rows are 108.8 ns against cgo's 225.9 ns and 205.7 ns against
   cgo's 119.8 ns: the failure path is now 5.3 times its own success row instead
   of 35, and the lifecycle is 1.7 times cgo instead of 6.4. What remains on the
   lifecycle row is the handle's six allocations, not a lookup.
2. **Generated entry-point overhead.** Every call runs `cppgoOpen` and
   `ProbeStack` before the trampoline; measured separately at 964 MHz, the lock
   and the probe cost about as much as the `asm.Call` they guard. That
   decomposition has not been repeated at the performance clock, so the 15.9 ns
   trampoline figure cannot be subtracted from this run's 10.5 ns; what the row
   does say is that `cppgo_direct` is seven times a Go call, and the boundary
   around the call is about 9 ns. Emitter work.
3. **By-value POD arguments escape.** The generated code takes the address of
   every by-value parameter and also passes it to `KeepAlive`; `go build
   -gcflags=-m` reports `moved to heap: a`, `moved to heap: b` and `moved to
   heap: cppgoResult` for `Midpoint`. Three allocations per call against cgo's
   one, on both the 16- and the 24-byte row. Emitter work.
4. **The reentrant binding registers per call.** `RegisterCallback` copies the
   64-slot table under its lock before every call, and `ReleaseCallback` after
   it: 1200 B and 6 allocations per call, 499.2 ns against cgo's 90.4 ns — 5.5
   times cgo's callback. A registration at binding time is what the row asks
   for. Emitter work.

## Related

- The suite's own documentation, arm by arm: [`benchmarks/README.md`](../../benchmarks/README.md).
- The manual page it backs: [Performance](../../site/content/explanation/benchmarks.md).
