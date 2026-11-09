---
title: "Performance"
description: "What a call costs: cppgo against cgo and against calling Go directly."
weight: 28
---

cgo's per-call overhead is one of the reasons cppgo exists, so the comparison is
part of the product. This page compares the same C++ work, compiled once,
reached three ways: a plain Go function, cgo, and the package cppgo generates.

The comparison comes from the benchmark suite in
[`benchmarks/`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/README.md).
That page documents every arm, the raw `go test` output the numbers are taken
from, the ranges behind each median, and how to run it again.

## What each arm measures

| Arm | How Go reaches the function |
|---|---|
| `go_direct` | no C++: a plain Go function called directly (`//go:noinline`, so it is a real call) |
| `go_indirect` | the same Go function behind a `func` value — the baseline where a callback makes a value unavoidable |
| `cgo` / `cgo_thunk` | the C++ library through a hand-written `extern "C"` shim and cgo, with and without `noexcept` |
| `cppgo_direct` | the generated package, for a declaration the direct carrier can take |
| `cppgo_thunk` | the same arithmetic with `noexcept` removed, so the call pays the status and out-parameter shape a throwing declaration has |
| `cppgo` | the generated package, on the carrier its declaration gets |

Every arm calls the same compiled C++ library, built once with the library's own
flags. Nothing is mocked and nothing is reimplemented for an arm: where two arms
differ, it is because their declarations differ.

The cgo wrapper is the fastest correct wrapper for this work — borrowed Go
memory is passed as `(pointer, length)` rather than `C.CString`/`C.CBytes`, and
results are copied out once. The cppgo numbers are the generated API as shipped,
entry point included, because that is what a caller pays.

## Environment

- CPU: AMD Ryzen 7 6800HS with Radeon Graphics, 16 logical CPUs.
- OS and kernel: Linux 6.12 x86-64; Go 1.27; g++ (GCC) 16.2.1.
- Frequency scaling on: governor `powersave`, CPU 0 at 964830 kHz of a 3201000 kHz
  maximum when the run started.
- No tuning: no pinning, no governor change, no boost change, default `GOMAXPROCS`.
- Six runs per arm. Each cell is the **median** of those runs; the suite records
  the min–max and the allocations beside it.
- Each arm runs in its own process, with the cgo mode it needs: `CGO_ENABLED=0`
  for plain Go and cppgo, `CGO_ENABLED=1` for cgo.

Absolute nanoseconds move with the machine and its clock state. The ratios are
the reproducible claim.

## The crossing

`bench::add(int64, int64)` — one call, nothing else:

| Arm | Median ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `go_direct` | 6.33 | 0 | 0 |
| `go_indirect` | 6.29 | 0 | 0 |
| `cgo` | 107.8 | 0 | 0 |
| `cgo_thunk` | 157.9 | 8 | 1 |
| `cppgo_direct` | 40.9 | 0 | 0 |
| `cppgo_thunk` | 94.2 | 8 | 1 |

On the same call, the direct carrier costs 40.9 ns against cgo's 107.8 ns: about
2.6× faster. The thunk shape costs 94.2 ns, close to cgo's 107.8 ns and far from
the direct carrier's price — the difference is the status return and the
out-parameter's allocation, not the call into C++.

## The rest of the work

Medians from the same run. The last column puts the cgo and cppgo medians side by
side.

| What is measured | Go | cgo | cppgo | cppgo vs cgo |
|---|---:|---:|---:|---:|
| `midpoint`: 16-byte POD by value in and out | 6.45 | 163.6 | 190.0 | 1.2× slower |
| `blend`: 24-byte POD by value, result in memory | 7.33 | 199.6 | 211.7 | 1.1× slower |
| `describe`: `std::string_view` in, `std::string` out | 309.4 | 1190 | 795.2 | 1.5× faster |
| `doubled`, 8 elements: `std::vector` in and out | 104.3 | 786.2 | 430.1 | 1.8× faster |
| `doubled`, 1024 elements | 5198 | 8728 | 9510 | 1.1× slower |
| `throw`: an exception becomes a Go error | 75.9 | 5522 | 7405 | 1.3× slower |
| `parse` succeeds (`-status`) | 9.51 | 134.7 | 71.8 | 1.9× faster |
| `parse` fails (`-status`) | 143.8 | 912.4 | 2518 | 2.8× slower |
| callback round trip: C++ calls a Go function once | 11.6 (`go_indirect`) | 356.3 | 1776 | 5.0× slower |
| construct and destroy an opaque object | 32.3 | 466.9 | 2974 | 6.4× slower |
| one `noexcept` method call on a live object | 6.35 | 153.0 | 43.3 | 3.5× faster |

## What the numbers say

- **The direct carrier is the cheapest way to call C++ from Go.** It beats cgo by
  2.6× on the same call, and it beats cgo on a method call, on an in/out string,
  on a small vector, and on a successful status-returning call.
- **A thunk is close to cgo, not to the direct carrier.** A declaration that has
  to report a status or can throw pays for it: 94.2 ns against 40.9 ns on the
  same arithmetic. On the standard-library paths, where the copy dominates, cppgo
  is faster than cgo anyway.
- **Where the work dominates, the boundary stops mattering.** At 1024 elements
  cgo and cppgo are within 10% of each other, because the vector copy is most of
  the time.
- **Two paths are currently slower than they should be.** A callback is 5× cgo's
  cost because the generated binding registers and releases it per call; creating
  and closing an object is 6.4× cgo's because every `Close` takes the error path,
  which resolves a symbol per call. Both costs are in the generated binding, not
  in the C++ call.
- **By-value arguments allocate.** A POD passed by value costs 3 allocations
  against cgo's 1; the small row's time does not show it, but it is garbage per
  call.
