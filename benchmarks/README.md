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
re-derived from the table above, the code it names, or the commands below.

**The direct carrier is the cheapest way to call C++ from Go.** `BenchmarkCall`
is the crossing alone on `bench::add`, and `cppgo_direct` is more than twice as
fast as `cgo` on it. That is the project's reason to exist, measured on
identical work rather than asserted.

**Most of the direct call is not the trampoline.** `DESIGN.md` prices the bare
trampoline at 15.9 ns/op at 964 MHz, which is the condition this run was made
in (see the environment block). `cppgo_direct` is several times that, and the
generated entry point is why: `cppgoOpen` takes a process mutex and `ProbeStack`
runs, on every call, before the trampoline. Measured separately on this machine
the lock and the probe together cost more than the `asm.Call` they guard. This
challenges *"the Go-side cost is the same trampoline"*: the trampoline is the
cheap part; the generated wrapper around it is not.

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
heap: b` and `moved to heap: cppgoResult` for `Midpoint`. Three small
allocations per call is the largest single cost in that row.

**The failure paths are dominated by a symbol lookup.** `BenchmarkStatusFailure`
is `BenchmarkStatusOK` with a non-zero status and nothing else changed, and it
is tens of times slower; `BenchmarkThrow` carries the same penalty on top of the
C++ exception. The error is built by `cppgo.LastError`, which asks
`Library.Symbol` for `cppgo_last_error` on every failure, and `Library.Symbol`
calls `dlsym` with no cache. The same lookup is why `BenchmarkObjectNewClose` is
far slower than the cgo arm: the generated `Close` always calls
`cppgoCloseError`, which always calls `LastError`, even when the destructor
failed at nothing.

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
- Frequency: governor powersave; cpu0 at 964840 kHz (cpuinfo max 3201000 kHz) when the run started
- Tuning: none — no pinning, no governor change, no boost change, default GOMAXPROCS
- Runs: 6 per arm; each cell is the median of those runs, the range is their min–max
- Command: go test -run '^$' -bench . -benchmem -count=6 -timeout=30m
- Tree: 499a4ff
- Date: 2026-11-08T23:36:45Z

<!-- END ENVIRONMENT -->

<!-- BEGIN TABLE (generated by cmd/benchtable) -->

### `BenchmarkCall`

the crossing floor: `bench::add(int64,int64)` and `bench::add_thunk`, the same arithmetic with and without `noexcept`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 6.58 | 6.50–6.64 | 0 | 0 | 1.00× |
| `go_indirect` | 6.53 | 6.46–7.33 | 0 | 0 | 0.99× |
| `cgo` | 121.2 | 111.4–129.0 | 0 | 0 | 18.42× |
| `cgo_thunk` | 165.3 | 159.7–174.9 | 8 | 1 | 25.12× |
| `cppgo_direct` | 40.6 | 39.8–42.0 | 0 | 0 | 6.18× |
| `cppgo_thunk` | 95.3 | 93.1–98.8 | 8 | 1 | 14.48× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkMidpoint`

a 16-byte POD by value in and out: `bench::midpoint(Point,Point)`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 11.2 | 9.43–16.1 | 0 | 0 | 1.00× |
| `cgo` | 194.9 | 174.1–213.9 | 16 | 1 | 17.41× |
| `cppgo` | 189.6 | 187.3–195.3 | 48 | 3 | 16.93× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkBlend`

a 24-byte POD by value with a result above the register class: `bench::blend(Sample,Sample,double) noexcept`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 8.90 | 7.93–10.8 | 0 | 0 | 1.00× |
| `cgo` | 204.9 | 197.1–224.5 | 24 | 1 | 23.03× |
| `cppgo` | 212.9 | 203.8–276.2 | 72 | 3 | 23.92× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkDescribe`

`std::string_view` in, `std::string` out: `bench::describe`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 317.0 | 312.1–381.3 | 24 | 2 | 1.00× |
| `cgo` | 1196 | 1166–1336 | 32 | 3 | 3.77× |
| `cppgo` | 808.4 | 798.2–877.6 | 32 | 3 | 2.55× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkDoubled/n=8`

`std::vector<double>` in and out, 8 elements: `bench::doubled`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 87.5 | 85.7–91.3 | 64 | 1 | 1.00× |
| `cgo` | 765.7 | 741.7–820.5 | 80 | 3 | 8.75× |
| `cppgo` | 519.3 | 457.0–658.4 | 80 | 3 | 5.94× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkDoubled/n=1024`

`std::vector<double>` in and out, 1024 elements: `bench::doubled`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 4914 | 4784–5397 | 8192 | 1 | 1.00× |
| `cgo` | 11069 | 9847–15049 | 8208 | 3 | 2.25× |
| `cppgo` | 9560 | 9234–11642 | 8208 | 3 | 1.95× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkThrow`

the throwing failure path: `bench::ratio(a, 0)` throws `std::domain_error`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 73.5 | 72.0–74.1 | 16 | 1 | 1.00× |
| `cgo` | 6248 | 5688–6750 | 124 | 6 | 84.97× |
| `cppgo` | 7789 | 7499–8695 | 176 | 9 | 105.92× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkStatusOK`

the `-status` convention's success path: `bench::parse("bench")` returns 0

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 9.84 | 9.49–10.2 | 0 | 0 | 1.00× |
| `cgo` | 140.5 | 135.4–200.9 | 0 | 0 | 14.27× |
| `cppgo` | 81.6 | 74.2–83.5 | 0 | 0 | 8.29× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkStatusFailure`

the `-status` convention's failure path: `bench::parse("!bench")` returns 3

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 148.2 | 148.1–151.1 | 16 | 1 | 1.00× |
| `cgo` | 1118 | 953.0–1210 | 104 | 4 | 7.54× |
| `cppgo` | 2546 | 2507–2622 | 176 | 9 | 17.18× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkReentrant`

the C++ → Go round trip: `bench::fold(fn, 3, 4)` calls the Go function once

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_indirect` | 12.0 | 11.6–12.1 | 0 | 0 | — |
| `cgo` | 362.4 | 356.3–369.1 | 4 | 1 | — |
| `cppgo` | 1845 | 1734–1921 | 1200 | 6 | — |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkObjectNewClose`

the object lifecycle: construct an opaque `bench::Counter`, then destroy it

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 32.8 | 32.1–33.8 | 8 | 1 | 1.00× |
| `cgo` | 539.5 | 486.6–648.7 | 8 | 1 | 16.45× |
| `cppgo` | 4764 | 3121–6369 | 240 | 12 | 145.22× |

6 run(s) per arm; medians, and the spread of the same runs.

### `BenchmarkObjectGet`

one method call on a live object: `bench::Counter::get() const noexcept`

| arm | median ns/op | min–max ns/op | B/op | allocs/op | vs `go_direct` |
|---|---:|---:|---:|---:|---:|
| `go_direct` | 6.53 | 6.51–6.71 | 0 | 0 | 1.00× |
| `cgo` | 153.4 | 149.4–158.3 | 0 | 0 | 23.51× |
| `cppgo` | 56.3 | 47.1–68.3 | 0 | 0 | 8.63× |

6 run(s) per arm; medians, and the spread of the same runs.

<!-- END TABLE -->
