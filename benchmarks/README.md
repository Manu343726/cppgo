# benchmarks

The comparison the project exists for: the same C++ work reached three ways,
in the same conditions, on one machine.

| arm | how Go reaches the function | `CGO_ENABLED` | package |
|---|---|---|---|
| `go_direct` | no C++: a plain Go function, called directly (`//go:noinline`, so it is a real call) | 0 | `gobase` |
| `go_indirect` | the same Go function behind a `func` value | 0 | `gobase` |
| `cgo` / `cgo_thunk` | the C++ library through a hand-written `extern "C"` shim and cgo | **1** | `cgo` |
| `cppgo` / `cppgo_direct` / `cppgo_thunk` | the C++ library through the package `cppgo gen` wrote | **0** | `cppgo` |

`cppgo_direct` and `cppgo_thunk` are the two carriers on the same arithmetic
(`BenchmarkCall`); every other group uses the carrier its declaration gets and
labels the arm `cppgo`. `BenchmarkReentrant` has no `go_direct` arm — the Go
baseline there is `go_indirect`, because a callback is necessarily called
through a value.

Every arm calls **the same `build/libbench.so`**, compiled once by
`src/bench.cpp` with the library's own flags. Nothing is mocked, nothing is
reimplemented for an arm, and no declaration is shaped to favour one: where two
arms differ it is because a carrier rule in `DESIGN.md` makes them differ, and
the C++ comment on the declaration says which rule.

This file is the suite's own documentation, for the people who run and review
it. The user-facing comparison is written from `results/` here (issue #38), and
`docs/dev/benchmarks.md` is where the published methodology and its provenance
live; neither measures anything of its own.

## Method

1. **One library, one set of symbols.** `make lib` compiles
   `include/bench/bench.hpp` + `src/bench.cpp` into `build/libbench.so` with
   `-std=c++17 -O2 -fPIC` and its own soname. The cgo package's shim links
   against it; the cppgo package's generated shim links against it; neither
   compiles the work itself.
2. **Every arm computes the same values.** `internal/benchdata` is the plain-Go
   reference, and each FFI arm's tests compare its answers against it — the
   strings byte for byte, the slices element by element. A benchmark cannot
   price a call that returns garbage.
3. **Each arm is its own process.** `go test` runs one arm package at a time,
   with the CGO mode that arm needs, so the cgo runtime cannot inflate a
   measurement that does not use it.
4. **`-benchmem` and `-count=N` on every arm.** The table shows the **median**
   ns/op across those runs, the run's own min–max beside it, and the median
   bytes and allocations per operation. A number whose range is wide is visibly
   wide; nothing is averaged away.
5. **No tuning.** Default `GOMAXPROCS`, no CPU pinning, no governor, no boost
   and no frequency change. Frequency scaling is on, so absolute nanoseconds
   move with the machine and the *ratios* are the reproducible claim. The
   environment block below is read from the machine at the start of the run.
6. **The table is generated.** `make bench` writes `results/*.txt` (the raw
   `go test` output, committed) and rewrites the block below from them.
   `make table` re-renders from the committed results without running anything.
   No number in this file is typed by hand.

What each arm pays for, and what it does not:

- The **cgo** wrapper is the fastest correct cgo wrapper for this work: borrowed
  Go memory is passed as `(pointer, length)` instead of `C.CString`/`C.CBytes`,
  and results are copied out once. A `CString`-based wrapper pays an extra
  allocation and a copy per string and is not what the numbers below show.
- The **cppgo** numbers are the *generated API as shipped*, not the bare
  trampoline: every call goes through the generated entry point (`cppgoOpen`,
  `ProbeStack`, then the trampoline or thunk), because that is what a caller
  pays. `DESIGN.md` quotes the trampoline alone at 15.9 ns/op; the difference
  between that number and `BenchmarkCall/cppgo_direct` is the wrapper.
- **`cppgo_thunk` and `cgo_thunk`** are the same arithmetic as the direct call
  with `noexcept` removed, so the row prices the status/out-parameter shape
  itself; the NOSPLIT trampoline is the same one the direct arm uses.
- **Failure rows measure the whole Go-visible operation**, the error value
  included: the C++ side's exception or status, the shim's catch, the message
  copy, and the Go error the binding returns. The three arms construct
  different error types by design, so the row is what a caller of each API
  pays, not a byte-identical computation.

## Layout

```
include/bench/bench.hpp   the work under test, and the carrier rule per declaration
src/bench.cpp             its definitions; compiled into build/libbench.so
cppgo/                    `cppgo gen` output (committed) + the cppgo arm's tests
cgo/                      the hand-written extern "C" shim + cgo wrappers + tests
gobase/                   the plain-Go arm, benchmarking internal/benchdata
internal/benchdata/       the reference implementations and fixtures
cmd/benchtable/           the table generator (the only writer of the block below)
scripts/run.sh            run every arm, save the raw output, regenerate this file
results/                  the raw `go test -bench` output the table was built from
```

## Reproducing it

```console
$ make gen        # build libbench.so, generate the cppgo package, compile the shim
$ make test       # both cgo modes
$ make bench      # the matrix; rewrites the generated block in this file
```

or, from the repository root, `make benchmarks`.

A single arm by hand:

```console
# the plain Go baseline and the cppgo arm: no cgo anywhere
$ CGO_ENABLED=0 go test -run '^$' -bench . -benchmem -count=6 ./gobase ./cppgo

# the cgo arm: needs cgo, and the library built first
$ CGO_ENABLED=1 go test -run '^$' -bench . -benchmem -count=6 ./cgo
```

The generated package loads two objects — `cppgo/libcppgo_benchcpp.so` (the
shim `cppgo gen` compiled) and `build/libbench.so` (the work). `CPPGO_BENCH_LIB`
and `CPPGO_BENCH_SHIM` override their paths; the defaults are what `make gen`
leaves behind.

Requirements: Go 1.25+, a C++17 compiler, libclang (generation only).
`make` builds all of it; the benchmark tables additionally want an idle
machine, and say so where they present the numbers.

## What the numbers say

The table is the evidence; this section is what the rows mean, and where they
contradict a claim the project makes elsewhere. Every statement here can be
re-derived from the table, the code it names, or the commands above.

**The direct carrier is the cheapest way to call C++ from Go.** `BenchmarkCall`
is the crossing alone on `bench::add`, and `cppgo_direct` is more than twice as
fast as `cgo` on it. That is the project's reason to exist, measured on
identical work rather than asserted.

**Most of the direct call is not the trampoline.** `DESIGN.md` prices the bare
trampoline at 15.9 ns/op at 964 MHz — the frequency this run was made at (see
the environment block). The `cppgo_direct` row is more than twice that figure,
and the generated entry point is why: every call runs `cppgoOpen`, which takes a
process mutex, and then `ProbeStack`, before it reaches the trampoline. Measured
separately on this machine, the lock and the probe together cost about as much
as the `asm.Call` they guard. This challenges *"the Go-side cost is the same
trampoline"*: the trampoline is the cheap part, and the wrapper around it is not.

**A thunk costs about what cgo costs.** `cppgo_thunk` is `cppgo_direct`'s
arithmetic with `noexcept` removed, and it lands at cgo's price rather than the
direct carrier's. What is paid is the status return, the out-parameter (whose
storage escapes to the heap) and one extra allocation — not the C++ call frame
the carrier table in `DESIGN.md` names.

**By-value POD arguments allocate.** `BenchmarkMidpoint` and `BenchmarkBlend`
show the generated binding at three allocations per call against cgo's one. The
generated code takes the address of every by-value parameter to hand it to the
trampoline, and then passes the same parameters to `KeepAlive`; `go build
-gcflags=-m` on the generated package reports `moved to heap: a`, `moved to
heap: b` and `moved to heap: cppgoResult` for `Midpoint`. The allocation is
small and the row's time does not show it, but it is garbage produced per call
that cgo's crossing does not produce — on a path a caller may be running in a
loop.

**The failure paths are dominated by a symbol lookup.** `BenchmarkStatusFailure`
is `BenchmarkStatusOK` with a non-zero status and nothing else changed, and it
is tens of times slower; `BenchmarkThrow` carries the same penalty on top of the
C++ exception. The error is built by `cppgo.LastError`, which asks
`Library.Symbol` for `cppgo_last_error` on every failure, and `Library.Symbol`
calls `dlsym` with no cache. The same lookup is why `BenchmarkObjectNewClose` is
far slower than the cgo arm: the generated `Close` always calls
`cppgoCloseError`, which always calls `LastError`, even when nothing failed.

**The reentrant binding registers a callback per call.** `BenchmarkReentrant`
shows the generated binding at several allocations and more than a kilobyte per
call, against cgo's one small allocation. The emitted code calls
`RegisterCallback` before every call and `ReleaseCallback` after it, and
registration copies the process-wide 64-slot table under its lock
(`internal/callback`). A binding that registered once, rather than per call, is
what that row asks for.

**Where the work dominates, the boundary stops mattering.** At `n=1024`
`BenchmarkDoubled`'s gap between the Go baseline and the two FFI arms falls from
several times to roughly double, even though the FFI arms are still slower: the
vector construct-and-copy is most of the time, and the crossing is a fraction of
it. Passing `(pointer, length)` instead of copying is what keeps it that way,
which is the case the `stats` example teaches.

## The numbers

The block below is generated by `cmd/benchtable` from `results/*.txt`, which
`scripts/run.sh` writes from one real run. `—` in `vs go_direct` means the group
has no plain-Go call arm to compare against.

<!-- BEGIN ENVIRONMENT (generated by cmd/benchtable) -->

- CPU: AMD Ryzen 7 6800HS with Radeon Graphics, 16 logical CPUs
- Kernel: Linux 6.12.108-1-MANJARO x86_64
- Go: go version go1.27.0-X:nodwarf5 linux/amd64
- C++ compiler: g++ (GCC) 16.2.1 20260810
- cgo: gobase and cppgo run with CGO_ENABLED=0, cgo runs with CGO_ENABLED=1
- Frequency: governor performance; cpu0 at 3602458 kHz (cpuinfo max 4785000 kHz) when the run started
- Tuning: none — no pinning, no governor change, no boost change, default GOMAXPROCS
- Runs: 6 per arm; each cell is the median of those runs, the range is their min–max
- Command: go test -run '^$' -bench . -benchmem -count=6 -timeout=30m
- Tree: 532703d
- Date: 2026-11-09T10:51:23Z

<!-- END ENVIRONMENT -->

<!-- BEGIN TABLE (generated by cmd/benchtable) -->

### `BenchmarkCall`

the crossing floor: `bench::add(int64,int64)` and `bench::add_thunk`, the same arithmetic with and without `noexcept`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 1.50 | 1.45–1.55 | 0 | 0 | 1.00× |
| `go_indirect` | 1.58 | 1.51–1.59 | 0 | 0 | 1.06× |
| `cgo` | 29.6 | 27.6–30.9 | 0 | 0 | 19.75× |
| `cgo_thunk` | 42.2 | 41.1–43.4 | 8 | 1 | 28.20× |
| `cppgo_direct` | 10.5 | 9.88–12.1 | 0 | 0 | 6.99× |
| `cppgo_thunk` | 29.2 | 23.5–39.4 | 8 | 1 | 19.53× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkMidpoint`

a 16-byte POD by value in and out: `bench::midpoint(Point,Point)`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 1.60 | 1.55–1.66 | 0 | 0 | 1.00× |
| `cgo` | 45.1 | 41.7–54.6 | 16 | 1 | 28.27× |
| `cppgo` | 56.5 | 55.4–59.2 | 48 | 3 | 35.45× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkBlend`

a 24-byte POD by value with a result above the register class: `bench::blend(Sample,Sample,double) noexcept`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 2.00 | 1.77–2.13 | 0 | 0 | 1.00× |
| `cgo` | 50.3 | 48.3–54.5 | 24 | 1 | 25.11× |
| `cppgo` | 63.0 | 57.5–73.6 | 72 | 3 | 31.44× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkDescribe`

`std::string_view` in, `std::string` out: `bench::describe`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 84.2 | 75.5–91.8 | 24 | 2 | 1.00× |
| `cgo` | 319.9 | 292.7–373.0 | 32 | 3 | 3.80× |
| `cppgo` | 245.2 | 228.8–266.5 | 32 | 3 | 2.91× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkDoubled/n=8`

`std::vector<double>` in and out, 8 elements: `bench::doubled`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 23.4 | 22.2–68.8 | 64 | 1 | 1.00× |
| `cgo` | 215.6 | 183.9–271.9 | 80 | 3 | 9.21× |
| `cppgo` | 117.9 | 109.4–122.7 | 80 | 3 | 5.04× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkDoubled/n=1024`

`std::vector<double>` in and out, 1024 elements: `bench::doubled`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 1214 | 1199–1225 | 8192 | 1 | 1.00× |
| `cgo` | 2716 | 2485–5241 | 8208 | 3 | 2.24× |
| `cppgo` | 2455 | 2399–2610 | 8208 | 3 | 2.02× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkThrow`

the throwing failure path: `bench::ratio(a, 0)` throws `std::domain_error`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 18.6 | 18.5–19.8 | 16 | 1 | 1.00× |
| `cgo` | 1386 | 1356–1400 | 124 | 6 | 74.57× |
| `cppgo` | 1301 | 1276–2559 | 52 | 3 | 70.02× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkStatusOK`

the `-status` convention's success path: `bench::parse("bench")` returns 0

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 2.51 | 2.47–2.55 | 0 | 0 | 1.00× |
| `cgo` | 36.3 | 35.0–37.3 | 0 | 0 | 14.45× |
| `cppgo` | 20.5 | 19.2–20.7 | 0 | 0 | 8.15× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkStatusFailure`

the `-status` convention's failure path: `bench::parse("!bench")` returns 3

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 35.1 | 34.8–35.9 | 16 | 1 | 1.00× |
| `cgo` | 225.9 | 223.4–275.1 | 104 | 4 | 6.43× |
| `cppgo` | 108.8 | 103.5–114.8 | 56 | 3 | 3.10× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkReentrant`

the C++ → Go round trip: `bench::fold(fn, 3, 4)` calls the Go function once

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_indirect` | 2.94 | 2.85–2.99 | 0 | 0 | — |
| `cgo` | 90.4 | 88.4–98.0 | 4 | 1 | — |
| `cppgo` | 499.2 | 453.8–509.0 | 1200 | 6 | — |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkObjectNewClose`

the object lifecycle: construct an opaque `bench::Counter`, then destroy it

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 8.21 | 8.17–8.80 | 8 | 1 | 1.00× |
| `cgo` | 119.8 | 117.5–130.9 | 8 | 1 | 14.59× |
| `cppgo` | 205.7 | 200.9–212.9 | 120 | 6 | 25.05× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkObjectGet`

one method call on a live object: `bench::Counter::get() const noexcept`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 1.58 | 1.54–1.66 | 0 | 0 | 1.00× |
| `cgo` | 38.0 | 36.6–39.5 | 0 | 0 | 24.12× |
| `cppgo` | 11.9 | 11.2–12.9 | 0 | 0 | 7.51× |

6 run(s) per arm; medians, and the spread of the same runs.

<!-- END TABLE -->
