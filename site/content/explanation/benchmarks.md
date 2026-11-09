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
That page documents the raw `go test` output the numbers are taken from, the
ranges behind each median, and how to run it again.

## The C++ that is measured

One header is the source of every measured call,
[`benchmarks/include/bench/bench.hpp`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/include/bench/bench.hpp).
Each row of the tables below names one of these declarations:

```cpp
namespace bench {

std::int64_t add(std::int64_t a, std::int64_t b) noexcept;

std::int64_t add_thunk(std::int64_t a, std::int64_t b);

struct Point {
    double x;
    double y;
};

struct Sample {
    double a;
    double b;
    double c;
};

Point midpoint(Point a, Point b);

Sample blend(Sample a, Sample b, double t) noexcept;

std::string describe(std::string_view prefix, double value);

std::vector<double> doubled(const std::vector<double> &values);

std::int32_t ratio(std::int32_t a, std::int32_t b);

std::int32_t parse(const std::string &text);

std::int32_t fold(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                  std::int32_t a, std::int32_t b);

class Counter {
public:
    explicit Counter(std::int64_t start);

    ~Counter();

    std::int64_t get() const noexcept;

    void bump(std::int64_t by) noexcept;

private:
    std::int64_t value_;
};

} // namespace bench
```

`add` and `add_thunk` are the same arithmetic; only `noexcept` differs, which is
what moves the second one to the shim. `ratio` throws `std::domain_error` on a
zero divisor, and `parse` reports failure with a 32-bit status code. Every arm
calls this one compiled `libbench.so`.

## The arms

The names in the tables are the benchmark's sub-test names. Each arm is a Go
package with its own benchmark file, and the statements below are the ones inside
its `b.Loop()`.

### Plain Go — `go_direct`, `go_indirect`

The reference implementation, with no foreign call anywhere.
[`benchmarks/gobase/bench_test.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/gobase/bench_test.go):

```go
sinkInt64 = benchdata.Add(addA, addB)
sinkInt64 = benchdata.AddFn(addA, addB)
sinkPoint = benchdata.Midpoint(pointA, pointB)
sinkSample = benchdata.Blend(sampleA, sampleB, blendT)
sinkString = benchdata.Describe(describePrefix, describeValue)
sinkFloats = benchdata.Doubled(values)
sinkInt32, sinkErr = benchdata.Ratio(ratioA, ratioZeroDivisor)
sinkErr = benchdata.Parse(statusText)
sinkErr = benchdata.Parse("!" + statusText)
sinkInt32 = benchdata.Fold(callback, 3, 4)
counter := benchdata.NewCounter(7)
counter.Bump(1)
sinkInt64 = existingCounter.Get()
```

The functions are in
[`benchmarks/internal/benchdata/benchdata.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/internal/benchdata/benchdata.go);
`Add` carries `//go:noinline`, so `go_direct` is a real call and not inlined
work, and `AddFn` is the same function behind a `func` value:

```go
//go:noinline
func Add(a, b int64) int64 { return a + b }

var AddFn = func(a, b int64) int64 { return a + b }
```

### cgo — `cgo`, `cgo_thunk`

The same calls, through the hand-written wrapper in
[`benchmarks/cgo`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cgo),
which pays cgo's crossing on every one of them:

```go
sinkInt64 = Add(addA, addB)
sinkInt64, sinkErr = AddThunk(addA, addB)
sinkPoint, sinkErr = Midpoint(pointA, pointB)
sinkSample = Blend(sampleA, sampleB, blendT)
sinkString, sinkErr = Describe(describePrefix, describeValue)
sinkFloats, sinkErr = Doubled(values)
sinkInt32, sinkErr = Ratio(ratioA, ratioZeroDivisor)
sinkErr = Parse(statusText)
sinkErr = Parse("!" + statusText)
sinkInt32, sinkErr = Fold(foldFnArg, 3, 4)
counter, err := NewCounter(7)
counter.Bump(1)
sinkInt64 = existingCounter.Get()
```

Those are wrappers, not the C calls. The crossing's two wrappers, from
[`benchmarks/cgo/cgo.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cgo/cgo.go):

```go
func Add(a, b int64) int64 {
	return int64(C.bench_add(C.int64_t(a), C.int64_t(b)))
}

func AddThunk(a, b int64) (int64, error) {
	var out C.int64_t
	if status := C.bench_add_thunk(C.int64_t(a), C.int64_t(b), &out); status != 0 {
		return 0, lastError("bench::add_thunk")
	}
	return int64(out), nil
}
```

and the `extern "C"` surface they call, from
[`benchmarks/cgo/bench_c.h`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cgo/bench_c.h):

```c
int64_t bench_add(int64_t a, int64_t b);
int32_t bench_add_thunk(int64_t a, int64_t b, int64_t *out);
```

Every other wrapper has one of those two shapes: a direct call with no status
when the declaration is `noexcept` (`Blend`), or the status-and-out-parameter
call when it can fail or returns a `std::` type (`Describe` and the rest).

### cppgo — `cppgo_direct`, `cppgo_thunk`, `cppgo`

The same calls again, from the generated package in
[`benchmarks/cppgo`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cppgo).
`cppgo_direct` and `cppgo_thunk` are the two halves of the crossing; the other
rows are labelled `cppgo` because the declaration decides its carrier:

```go
sinkInt64 = Add(addA, addB)
sinkInt64, sinkErr = AddThunk(addA, addB)
sinkPoint, sinkErr = Midpoint(pointA, pointB)
sinkSample = Blend(sampleA, sampleB, blendT)
sinkString, sinkErr = Describe(describePrefix, describeValue)
sinkFloats, sinkErr = Doubled(values)
sinkInt32, sinkErr = Ratio(ratioA, ratioZeroDivisor)
sinkErr = Parse(statusText)
sinkErr = Parse("!" + statusText)
sinkInt32, sinkErr = Fold(benchdata.Add32, 3, 4)
counter, err := NewCounter(7)
counter.Bump(1)
sinkInt64 = existingCounter.Get()
```

The declarations behind them, copied from
[`benchmarks/cppgo/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cppgo/zz_cppgo.gen.go):

```go
func Add(a int64, b int64) int64
func AddThunk(a int64, b int64) (int64, error)
func Midpoint(a Point, b Point) (Point, error)
func Blend(a Sample, b Sample, t float64) Sample
func Describe(prefix string, value float64) (string, error)
func Doubled(values []float64) ([]float64, error)
func Ratio(a int32, b int32) (int32, error)
func Parse(text string) error
func Fold(fn func(int32, int32) int32, a int32, b int32) (int32, error)
func NewCounter(start int64) (*Counter, error)
func (c *Counter) Bump(by int64)
func (c *Counter) Get() int64
```

The generated package implements the same call shape as the other two arms:
where a declaration can fail the Go signature carries an error, and where it
cannot — `Add`, `Blend`, `Bump`, `Get` — it does not.

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

`bench::add` and `bench::add_thunk` — one call, nothing else:

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

Medians from the same run. Each row is one declaration from the header above, and
the arms are the ones whose calls are shown above it. The last column puts the
cgo and cppgo medians side by side.

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
| `fold`: C++ calls a Go function once | 11.6 (`go_indirect`) | 356.3 | 1776 | 5.0× slower |
| `Counter`: construct and destroy an opaque object | 32.3 | 466.9 | 2974 | 6.4× slower |
| `Counter::get`: one `noexcept` method call on a live object | 6.35 | 153.0 | 43.3 | 3.5× faster |

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

## Related

- The suite, its raw output and how to run it: [`benchmarks/README.md`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/README.md).
- Which construct gets which carrier: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- What each `cppgo` call becomes at the ABI: [Architecture]({{< ref "explanation/architecture.md" >}}).
