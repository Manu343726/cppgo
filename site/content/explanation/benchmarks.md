---
title: "Performance"
description: "What a call costs: cppgo against cgo and against calling Go directly."
weight: 28
---

cgo's per-call overhead is one of the reasons cppgo exists, so the comparison is
part of the product. This page measures the same C++ work, compiled once into one
shared object, reached three ways: a plain Go function, cgo through a
hand-written `extern "C"` wrapper, and the package cppgo generates.

Four words carry the page. An **arm** is one of the implementations being
compared — one Go package, one row of a results table. The **crossing** is the
moment a call leaves Go and enters C++, and what it costs is what most rows
isolate. A **foreign call** is a call from Go into code the Go toolchain never
compiled. **FFI**, *foreign function interface*, is the general name for calling
into another language at all; the page says "the FFI arms" where it means the cgo
and cppgo rows of a scenario, as opposed to its plain-Go row.

Every scenario below stands on its own: what it measures, the code of each arm,
the results, and what the numbers mean. The scenarios are independent of each
other, and a reader who needs one does not have to read the others.

Every call's result is stored in a package-level *sink* variable (`sinkInt64`,
`sinkErr`, `sinkPoint` and so on, visible in the code below). A benchmark whose
result nobody keeps can be optimised away by the compiler, and a call the
compiler removed measures nothing.

## Reading the tables

- **ns/op** is the time one call takes. The table shows the **median** of the
  runs, and beside it the **min–max** range of those same runs. When two arms'
  ranges overlap, the difference between them is not a result.
- **B/op** and **allocs/op** are the bytes allocated on the Go heap and the
  number of allocations, per operation.
- **Iterations** is `b.N`, the loop count the benchmark ran with, quoted as the
  lowest–highest of its runs. It is context, not a result: the harness raises it
  until a run lasts about a second, so it tells a reader that a row was measured
  on 600 thousand calls or on 800 million.
- **Runs** is how many times the benchmark was repeated (`-count`), each a full
  measurement of its own in the same process as the others. Six runs is what the
  ranges above are made of.

Frequency scaling is on, so absolute nanoseconds move with the machine and with
its clock state. The ratios between arms in the same table, measured in the same
run, are the reproducible claim.

## The arms

The names in the tables are benchmark sub-tests. Each arm is a Go package that
calls the same compiled `libbench.so` — the C++ side built once, as a *shared
object*, a `.so` file the loader opens at run time — and each scenario quotes the
call the arm makes before its table. The three groups are introduced here, once.

### Plain Go — `go_direct` and `go_indirect`

No foreign call anywhere. The Go side is
[`benchmarks/internal/benchdata`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/internal/benchdata/benchdata.go),
the benchmark that calls it is
[`benchmarks/gobase`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/gobase)
(which is what the `// gobase` mark in the code below names), and the crossing
floor is one function:

```go
//go:noinline
func Add(a, b int64) int64 { return a + b }

var AddFn = func(a, b int64) int64 { return a + b }
```

`Add` carries `//go:noinline`, so `go_direct` is a real call and not inlined
work. `go_indirect` calls `AddFn`, the same function behind a `func` value: the
cost a closure, an interface method or a callback table adds before any
boundary is involved.

### cgo — `cgo` and `cgo_thunk`

The C++ library reached through a hand-written **shim** — a C++ file whose
functions present the library as something C can call, which is all cgo can reach
— with the Go side in
[`benchmarks/cgo/cgo.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cgo/cgo.go)
and the `extern "C"` surface in
[`benchmarks/cgo/bench_c.h`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cgo/bench_c.h)
(`extern "C"`: C++ functions with C linkage, so they have the C calling
convention and a name that is not mangled). cgo's crossing needs a status code and
an out-parameter around any call that can fail — a *status code* being an integer
return that says whether the call worked, an *out-parameter* a pointer the callee
writes its result into — so the wrapper comes in two shapes:

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

```c
int64_t bench_add(int64_t a, int64_t b);
int32_t bench_add_thunk(int64_t a, int64_t b, int64_t *out);
```

The wrapper is the fastest correct cgo wrapper for this work: borrowed Go memory
crosses as `(pointer, length)` instead of through `CString` — cgo's helper that
copies a Go string into memory C allocated — and results are copied out once.

### cppgo — `cppgo_direct`, `cppgo_thunk` and `cppgo`

The same calls from the package `cppgo gen` wrote,
[`benchmarks/cppgo`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cppgo).
The **carrier** a declaration gets — the route the generator chose for it: the
generated *trampoline* (a few instructions of Go assembly that jump straight to
the C++ symbol), a *thunk* in the shim (a C++ function that calls the declaration
and returns a status instead), or the reentrant call — decides the shape the Go
signature has, and the two shapes exist side by side only on the crossing, where
the same arithmetic is declared twice:

```go
func Add(a int64, b int64) int64
func AddThunk(a int64, b int64) (int64, error)
```

`cppgo_direct` is the `noexcept` declaration behind the generated trampoline and
`cppgo_thunk` is the same body without it, through the shim's status and
out-parameter. *`noexcept`* is the C++ keyword by which a function promises not to
throw, and it is what lets the trampoline call the function with no error channel
at all. Every other scenario uses the carrier its declaration gets and labels the
arm `cppgo`.

## Environment

- CPU: AMD Ryzen 7 6800HS with Radeon Graphics, 16 logical CPUs.
- OS and kernel: Linux 6.12 x86-64; Go 1.27; g++ (GCC) 16.2.1.
- Frequency: governor `performance` (the kernel's CPU frequency policy, set to
  run at the top of its range), boost enabled, cores up to 4785000 kHz; CPU 0
  was at 3602458 kHz when the run started.
- Default `GOMAXPROCS`; no CPU pinning.
- Six runs of every arm. Each cell is the median of those runs, with their
  min–max beside it.
- Each arm runs in its own process, with the cgo mode it needs: `CGO_ENABLED=0`
  for plain Go and cppgo, `CGO_ENABLED=1` for cgo.

## 1. The crossing: one call, nothing else

### The scenario

Two integers in, one out, no work worth pricing beside the call. It is the
floor of the comparison: what the boundary alone costs, before any data is
copied or any error is built. `bench::add` and `bench::add_thunk` are the same
arithmetic, declared with and without `noexcept`, so the difference between
their rows is the price of the status-and-out-parameter shape on identical work.

Six arms are compared: the two plain-Go calls, cgo and cgo's thunk form, and
cppgo's two carriers.

### The code

```cpp
// bench.hpp
std::int64_t add(std::int64_t a, std::int64_t b) noexcept;
std::int64_t add_thunk(std::int64_t a, std::int64_t b);
```

```go
// gobase
sinkInt64 = benchdata.Add(addA, addB)   // go_direct
sinkInt64 = benchdata.AddFn(addA, addB) // go_indirect

// cgo
sinkInt64 = Add(addA, addB)             // cgo
sinkInt64, sinkErr = AddThunk(addA, addB) // cgo_thunk

// cppgo
sinkInt64 = Add(addA, addB)             // cppgo_direct
sinkInt64, sinkErr = AddThunk(addA, addB) // cppgo_thunk
```

The declarations behind the last two, from
[`benchmarks/cppgo/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cppgo/zz_cppgo.gen.go):

```go
func Add(a int64, b int64) int64
func AddThunk(a int64, b int64) (int64, error)
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 748M–840M | 6 | 1.50 | 1.45–1.55 | 0 | 0 |
| `go_indirect` | 753M–810M | 6 | 1.58 | 1.51–1.59 | 0 | 0 |
| `cgo` | 34.5M–44.6M | 6 | 29.6 | 27.6–30.9 | 0 | 0 |
| `cgo_thunk` | 24.2M–29.6M | 6 | 42.2 | 41.1–43.4 | 8 | 1 |
| `cppgo_direct` | 100M–122M | 6 | 10.5 | 9.88–12.1 | 0 | 0 |
| `cppgo_thunk` | 26.5M–49.7M | 6 | 29.2 | 23.5–39.4 | 8 | 1 |

### What the numbers say

A Go call that does nothing costs **1.5 ns**. Every boundary is a multiple of
it, and the multiple is what the rest of this page is about.

**cppgo's direct carrier is the cheapest way across.** 10.5 ns against cgo's
29.6 ns on the same call: **2.8 times faster**, with the same zero allocations.
Against the Go call it is seven times, so the boundary costs roughly 9 ns around
a 1.5 ns call — the generated entry point checks that the library is open,
probes the stack, and enters the trampoline that reaches the C++ symbol.

**A thunk costs about what cgo costs.** `cppgo_thunk` is 29.2 ns against
`cppgo_direct`'s 10.5 ns on arithmetic that differs only by `noexcept`, and it
lands within a hair of `cgo`'s 29.6 ns. The status return and the out-parameter
account for the gap: the result escapes to the heap, which is the 8 B and one
allocation both thunk rows carry. The thunk shape is what a call that can fail
has to pay; it is not what the crossing itself costs.

**An indirect Go call is not measurably slower than a direct one.** 1.58 ns
against 1.50 ns, with the ranges nearly touching. A `func` value is not a cost
worth counting at this scale, which matters for the callback scenario below.

## 2. A 16-byte record passed by value

### The scenario

A two-`double` record, 16 bytes, passed by value in and out — the largest struct
the C ABI still hands over in registers. The *C ABI* is the calling surface a C
compiler produces, and on this platform its convention passes a record of up to
16 bytes in registers rather than in memory. It is the case where the boundary
has to copy a Go value into memory C++ can read, and copy the answer back.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk.

### The code

```cpp
// bench.hpp
struct Point {
    double x;
    double y;
};

Point midpoint(Point a, Point b);
```

```go
// gobase
sinkPoint = benchdata.Midpoint(pointA, pointB)

// cgo — the wrapper copies both arguments into the C ABI and the result back out
func Midpoint(a, b Point) (Point, error) {
	ca := C.struct_bench_point{x: C.double(a.X), y: C.double(a.Y)}
	cb := C.struct_bench_point{x: C.double(b.X), y: C.double(b.Y)}
	var out C.struct_bench_point
	if status := C.bench_midpoint(ca, cb, &out); status != 0 {
		return Point{}, lastError("bench::midpoint")
	}
	return Point{X: float64(out.x), Y: float64(out.y)}, nil
}

// cppgo — the generated signature
func Midpoint(a Point, b Point) (Point, error)
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 729M–801M | 6 | 1.60 | 1.55–1.66 | 0 | 0 |
| `cgo` | 23.3M–28.0M | 6 | 45.1 | 41.7–54.6 | 16 | 1 |
| `cppgo` | 19.1M–22.6M | 6 | 56.5 | 55.4–59.2 | 48 | 3 |

### What the numbers say

**Copying a record across costs more than copying two integers, on both
boundaries.** cgo is at 45.1 ns against 29.6 ns on the crossing, and cppgo at
56.5 ns against 10.5 ns. The record is copied into the caller's storage for the
C side and back out again, and that is most of the increase.

**Here cgo is the faster boundary, by 1.25 times.** 56.5 ns against 45.1 ns, and
the ranges do not overlap, so the gap is real. The reason is visible in the
allocation columns: cgo allocates the 16-byte result once, while the generated
binding allocates **48 B in three allocations**, because it takes the address of
every by-value parameter to hand it to the thunk, and those addresses escape to
the heap.

**Three allocations is garbage, per call.** 48 bytes is small enough that the
time barely shows it, but a caller looping over `Midpoint` produces three heap
objects per iteration that cgo does not produce. That is a cost a high-rate
caller should know about before moving a per-element C++ function behind the
boundary.

## 3. A record larger than the register class

### The scenario

A three-`double` record, 24 bytes, which is above the *register class*: the size
up to which the calling convention hands a record over in registers, and above
which it goes through memory the caller owns. The declaration is `noexcept`, so
the call cannot fail, and there is no error channel on any arm.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk.

### The code

```cpp
// bench.hpp
struct Sample {
    double a;
    double b;
    double c;
};

Sample blend(Sample a, Sample b, double t) noexcept;
```

```go
// gobase
sinkSample = benchdata.Blend(sampleA, sampleB, blendT)

// cgo — noexcept, so no status and no error: the result is written into out
func Blend(a, b Sample, t float64) Sample {
	ca := C.struct_bench_sample{a: C.double(a.A), b: C.double(a.B), c: C.double(a.C)}
	cb := C.struct_bench_sample{a: C.double(b.A), b: C.double(b.B), c: C.double(b.C)}
	var out C.struct_bench_sample
	C.bench_blend(ca, cb, C.double(t), &out)
	return Sample{A: float64(out.a), B: float64(out.b), C: float64(out.c)}
}

// cppgo — the generated signature
func Blend(a Sample, b Sample, t float64) Sample
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 537M–676M | 6 | 2.00 | 1.77–2.13 | 0 | 0 |
| `cgo` | 20.4M–25.1M | 6 | 50.3 | 48.3–54.5 | 24 | 1 |
| `cppgo` | 17.1M–21.7M | 6 | 63.0 | 57.5–73.6 | 72 | 3 |

### What the numbers say

**The pattern of the 16-byte record repeats, with more bytes.** cgo is at
50.3 ns and cppgo at 63.0 ns: cppgo is 1.25 times cgo's cost here too, and the
24-byte result is allocated once on the cgo side and three times on the cppgo
side, for 72 B against 24 B.

**The three allocations are the two arguments and the result.** cgo allocates the
24-byte result once; the generated binding takes the address of each by-value
parameter to hand it to the thunk, and the result's address escapes to the heap
as well, so all three end up there. It is the mechanism the scenario above showed
on two `double`s, repeated with three.

**`noexcept` is not a discount on the copy.** The generated `Blend` has no error
to return, yet it is slower than `Midpoint`, which has one: 63.0 ns against
56.5 ns. What it pays for is the third `double` in and out, not the status.

## 4. `std::string` out

### The scenario

A borrowed `std::string_view` in — a *view*: a pointer and a length that own
nothing — and a `std::string` built in C++ and copied out. This is the
standard-library case: the boundary has to construct a C++ string from bytes the
caller owns, then copy the result into Go storage and free the C++ side. The work
is the copy, not the crossing, and at 84 ns for the Go reference the two are the
same order of magnitude.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk.

### The code

```cpp
// bench.hpp
std::string describe(std::string_view prefix, double value);
```

```go
// gobase
sinkString = benchdata.Describe(describePrefix, describeValue)

// cgo — the string's bytes and length cross, no CString copy
func Describe(prefix string, value float64) (string, error) {
	var data unsafe.Pointer
	var size C.size_t
	status := C.bench_describe(
		(*C.char)(unsafe.Pointer(unsafe.StringData(prefix))), C.size_t(len(prefix)),
		C.double(value), &data, &size)
	if status != 0 {
		return "", lastError("bench::describe")
	}
	result := C.GoStringN((*C.char)(data), C.int(size))
	C.bench_free(data)
	return result, nil
}

// cppgo — the generated signature
func Describe(prefix string, value float64) (string, error)
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 13.5M–16.3M | 6 | 84.2 | 75.5–91.8 | 24 | 2 |
| `cgo` | 3.0M–4.2M | 6 | 319.9 | 292.7–373.0 | 32 | 3 |
| `cppgo` | 4.7M–5.4M | 6 | 245.2 | 228.8–266.5 | 32 | 3 |

### What the numbers say

**Both arms are a small multiple of the Go implementation**, 3.8 times for cgo
and 2.9 times for cppgo, because the work — building a C++ string, copying it
into a Go string, freeing the C++ side — dominates the crossing.

**Here cppgo is the faster boundary, by 1.3 times.** 245.2 ns against 319.9 ns,
ranges apart. The allocation columns are identical (32 B, three allocations), so
the difference is not in the copies or the string machinery: both sides do the
same work, and the generated crossing is the cheaper of the two.

**The Go reference allocates two objects, the FFI arms three.** The Go function
concatenates and formats into a fresh string; each FFI arm additionally allocates
the buffer the C++ string is copied through.

## 5. `std::vector<double>`, 8 elements

### The scenario

A `std::vector<double>` in and a new one out, at 8 elements. At this size the
allocation and the copy are comparable to the crossing, so the row shows both:
the input vector is constructed on the C++ side from the borrowed elements, and
the result is copied back into Go storage.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk.

### The code

```cpp
// bench.hpp
std::vector<double> doubled(const std::vector<double> &values);
```

```go
// gobase
sinkFloats = benchdata.Doubled(values)

// cgo — borrowed elements cross as a pointer and a length, the result is copied out
func Doubled(values []float64) ([]float64, error) {
	var data unsafe.Pointer
	var size C.size_t
	var ptr *C.double
	if len(values) != 0 {
		ptr = (*C.double)(unsafe.Pointer(&values[0]))
	}
	status := C.bench_doubled(ptr, C.size_t(len(values)), &data, &size)
	if status != 0 {
		return nil, lastError("bench::doubled")
	}
	var result []float64
	if size != 0 {
		result = append(result, unsafe.Slice((*float64)(data), int(size))...)
	}
	C.bench_free(data)
	return result, nil
}

// cppgo — the generated signature
func Doubled(values []float64) ([]float64, error)
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 17.7M–53.5M | 6 | 23.4 | 22.2–68.8 | 64 | 1 |
| `cgo` | 4.3M–6.8M | 6 | 215.6 | 183.9–271.9 | 80 | 3 |
| `cppgo` | 9.9M–11.2M | 6 | 117.9 | 109.4–122.7 | 80 | 3 |

### What the numbers say

**Here cppgo is nearly twice cgo's speed: 117.9 ns against 215.6 ns.** The two
allocate the same 80 B in the same three allocations — the 64-byte result slice
the Go caller receives, plus the boundary's scratch — so the difference is in
the crossing, and at this size the crossing is most of the cost.

**The Go reference's range is wide** (22.2–68.8 ns, one run at the top), which
is the fixture's own variance: an 8-element loop that does almost nothing is
dominated by the allocator and by whichever core ran it. Its median, 23.4 ns, is
a fifth of cppgo's.

**Eight elements is where the boundary is still most of the bill.** cppgo is 5
times the Go implementation; the next scenario shows what happens when the
elements outgrow it.

## 6. `std::vector<double>`, 1024 elements

### The scenario

The same call with 8 KiB of data: 1024 doubles in and a new 1024-double vector
out. The C++ side copies the input into a vector, doubles it and returns a new
one, and the boundary copies 8 KiB back into Go storage. At this size the memory
movement should be most of the time.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk. The code is the
one in the scenario above; only the fixture size changes.

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 836k–1.0M | 6 | 1214 | 1199–1225 | 8192 | 1 |
| `cgo` | 306k–505k | 6 | 2716 | 2485–5241 | 8208 | 3 |
| `cppgo` | 438k–502k | 6 | 2455 | 2399–2610 | 8208 | 3 |

### What the numbers say

**The boundary stops being the story.** cppgo is at 2.0 times the Go
implementation and cgo at 2.2 times, where at 8 elements the same two arms were
5 and 9 times it. The 8 KiB copy is most of the 2.4 µs, and it is the same copy
in all three arms.

**cppgo and cgo are within each other's reach here.** The medians differ by 11%
in cppgo's favour, and cgo's range has an outlier at 5241 ns; with that spread
the two boundaries are not distinguishable at this size. Both allocate the same
8208 B in three allocations: the 8 KiB result plus the bookkeeping the copy
needs.

**An 8 KiB operation should not go through the boundary per element.** The row
is here to show the size at which the comparison becomes about memory, and it is
the reason the caller-facing advice is to pass a buffer once rather than call
into C++ in a loop.

## 7. A C++ exception becomes a Go error

### The scenario

`bench::ratio` throws `std::domain_error` when the divisor is zero, and the call
is made with a zero divisor every time: the benchmark measures the whole
Go-visible operation, the error value included. On the C++ side that is a
`throw` — C++'s way of raising an exception — a `catch` in the shim, which
receives it at the language boundary, and a message stored for the Go side to
read; on the Go side, an error value built from that message.

Three arms: the Go function returning an error, cgo's wrapper, and cppgo's
thunk.

### The code

```cpp
// bench.hpp
std::int32_t ratio(std::int32_t a, std::int32_t b);
```

```cpp
// bench.cpp — the failure every iteration takes
std::int32_t ratio(std::int32_t a, std::int32_t b) {
    if (b == 0) {
        throw std::domain_error("bench: division by zero");
    }
    return a / b;
}
```

```go
// gobase
sinkInt32, sinkErr = benchdata.Ratio(ratioA, ratioZeroDivisor)

// cgo — the shim catches, the wrapper builds the error from the recorded message
sinkInt32, sinkErr = Ratio(ratioA, ratioZeroDivisor)

// cppgo — the generated signature
func Ratio(a int32, b int32) (int32, error)
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 58.4M–66.6M | 6 | 18.6 | 18.5–19.8 | 16 | 1 |
| `cgo` | 831k–921k | 6 | 1386 | 1356–1400 | 124 | 6 |
| `cppgo` | 605k–1.0M | 6 | 1301 | 1276–2559 | 52 | 3 |

### What the numbers say

**An exception is expensive, and it is the same expense on both boundaries.**
1.3 µs for cppgo and 1.39 µs for cgo, against 18.6 ns for the Go error: 70 to 75
times. That cost is the C++ unwinder, not the crossing — `cppgo_direct` calls the
same library at 10.5 ns.

**The two FFI arms are not distinguishable here.** The medians differ by 6% in
cppgo's favour, and cppgo's range reaches 2559 ns in one run. Both pay a thrown
exception per operation, which dominates whatever the boundary costs.

**cppgo's error value is smaller.** 52 B in three allocations against cgo's
124 B in six. The two APIs construct different error types by design, so the
row is what a caller of each pays, not a byte-identical computation: what is
comparable is the order of magnitude, and both are tens of times their own
successful call.

## 8. The `-status` convention, success

### The scenario

`cppgo gen -status bench::parse` turns an integer status into a Go `error`. The
C++ side returns 0 for text that does not start with `!`, and the binding
returns a nil error. This row is the success path of that convention: the call
succeeds, no error is built, and the only cost should be the crossing.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk.

### The code

```cpp
// bench.hpp
std::int32_t parse(const std::string &text);
```

```go
// gobase
sinkErr = benchdata.Parse(statusText)

// cgo — a non-zero status becomes an error, zero becomes nil
func Parse(text string) error {
	status := C.bench_parse(
		(*C.char)(unsafe.Pointer(unsafe.StringData(text))), C.size_t(len(text)))
	if status != 0 {
		return lastError("bench::parse")
	}
	return nil
}

// cppgo — the generated signature
func Parse(text string) error
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 470M–490M | 6 | 2.51 | 2.47–2.55 | 0 | 0 |
| `cgo` | 32.7M–35.2M | 6 | 36.3 | 35.0–37.3 | 0 | 0 |
| `cppgo` | 56.9M–66.6M | 6 | 20.5 | 19.2–20.7 | 0 | 0 |

### What the numbers say

**Nothing is allocated on any arm.** The status is an integer in a register and
the Go error is nil, so the row is the crossing plus a `std::string`
construction on the C++ side, and nothing else.

**cppgo is 1.8 times faster than cgo on it.** 20.5 ns against 36.3 ns, ranges
apart. It costs twice the crossing's 10.5 ns, which is the string C++ has to
construct from the borrowed bytes.

**The Go implementation is seven times cheaper still**, at 2.51 ns: it receives
a Go string and switches on its first byte, with nothing to construct.

## 9. The `-status` convention, failure

### The scenario

The same call, with a status of 3: the text starts with `!`, and both FFI
bindings build a Go error from the message the C++ side recorded. The benchmark
measures the whole Go-visible operation again, the error value included. It is
the row that shows what the convention costs when it is used for what it is for.

Three arms: the Go function, cgo's wrapper, and cppgo's thunk. The code is the
one in the scenario above; only the input changes, to the failing text.

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 31.2M–36.2M | 6 | 35.1 | 34.8–35.9 | 16 | 1 |
| `cgo` | 5.2M–5.4M | 6 | 225.9 | 223.4–275.1 | 104 | 4 |
| `cppgo` | 10.1M–11.9M | 6 | 108.8 | 103.5–114.8 | 56 | 3 |

### What the numbers say

**A failure is several times a success, not tens of times.** cppgo goes from
20.5 ns to 108.8 ns, 5.3 times its own success row; cgo goes from 36.3 ns to
225.9 ns, 6.2 times. The extra is the message copy and the error value, which
both arms build.

**cppgo is twice as fast as cgo on the failure path**: 108.8 ns against
225.9 ns, ranges apart, with 56 B in three allocations against 104 B in four.
The generated binding reads the failure message through a handle it resolves
once, so a failed call costs the message copy rather than a symbol lookup in the
library.

**The Go implementation's error is cheap by comparison**, at 35.1 ns, because
it formats a short string with no foreign call behind it: 3.1 times cheaper than
cppgo's failure row, where the successful row's ratio was 8 times.

## 10. C++ calls a Go function

### The scenario

`bench::fold` takes a `std::function` — C++'s owning wrapper around any callable,
whatever its concrete type — and calls it once. It is the C++ → Go round trip: Go
enters C++, the shim builds the `std::function` from the exported Go function (a
Go function marked `//export`, which gives it a C symbol C can call by name), and
the call comes back into Go. A callback is only legal on a carrier that runs
inside a foreign call, so the generated binding uses the reentrant carrier for
it.

Three arms: the Go function behind a `func` value (`go_indirect` — there is no
`go_direct` arm, because a callback is necessarily called through a value),
cgo's wrapper, and cppgo's reentrant binding.

### The code

```cpp
// bench.hpp
std::int32_t fold(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                  std::int32_t a, std::int32_t b);
```

```go
// gobase — the callback called through a func value
sinkInt32 = benchdata.Fold(callback, 3, 4)

// cgo — the exported Go function is stored, the shim builds the std::function
sinkInt32, sinkErr = Fold(foldFnArg, 3, 4)

// cppgo — the generated signature
func Fold(fn func(int32, int32) int32, a int32, b int32) (int32, error)
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_indirect` | 400M–439M | 6 | 2.94 | 2.85–2.99 | 0 | 0 |
| `cgo` | 13.1M–14.1M | 6 | 90.4 | 88.4–98.0 | 4 | 1 |
| `cppgo` | 2.3M–2.7M | 6 | 499.2 | 453.8–509.0 | 1200 | 6 |

### What the numbers say

**The round trip itself is cheap: 90.4 ns through cgo** against 2.94 ns for the
same call made entirely in Go. Entering C, building the `std::function` and
calling back into Go costs about 90 ns, and one 4-byte allocation.

**cppgo is 5.5 times cgo's cost on this row, and 170 times the Go call.** What
it pays is visible in the allocation columns: **1200 B in six allocations per
call**, against cgo's 4 B in one. The generated binding registers the Go
function with the callback table before the call and releases it after, and
every call pays a registration, a closure and a frame object — a per-call cost
that a binding registering once would not pay.

**A callback is the one case where the boundary's bookkeeping outgrows the
crossing.** `cppgo_direct` calls the same library at 10.5 ns; the reentrant
carrier adds a Go-side registration and a call through `runtime.cgocall` on top
of it, and that is the 500 ns.

## 11. The object lifecycle: construct and destroy

### The scenario

An opaque `bench::Counter`: the constructor runs on the C++ heap, Go owns a
handle, and closing it runs the destructor. *Opaque* means the type's layout is
invisible to Go, so Go holds only a *handle* — a pointer to storage C++
allocated; the *constructor* is the C++ function that creates an object and the
*destructor* the one that runs when it is destroyed. Each iteration constructs
one, bumps it, holds it in a sink and closes it, so the row is the whole
lifecycle of an object — allocation, construction, one method call and
destruction.

All three arms: the Go type, cgo's handle, and cppgo's generated object.

### The code

```cpp
// bench.hpp
class Counter {
public:
    explicit Counter(std::int64_t start);

    ~Counter();

    std::int64_t get() const noexcept;

    void bump(std::int64_t by) noexcept;

private:
    std::int64_t value_;
};
```

```go
// gobase — no foreign handle: the Go type is the allocator
counter := benchdata.NewCounter(7)
counter.Bump(1)
sinkCounter = counter
counter.Close()

// cgo and cppgo — construction and destruction can each fail
counter, err := NewCounter(7)
counter.Bump(1)
sinkCounter = counter
err = counter.Close()
```

The generated signatures, from
[`benchmarks/cppgo/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/cppgo/zz_cppgo.gen.go):

```go
func NewCounter(start int64) (*Counter, error)
func (c *Counter) Bump(by int64)
func (c *Counter) Close() error
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 133M–146M | 6 | 8.21 | 8.17–8.80 | 8 | 1 |
| `cgo` | 9.5M–10.2M | 6 | 119.8 | 117.5–130.9 | 8 | 1 |
| `cppgo` | 5.5M–6.1M | 6 | 205.7 | 200.9–212.9 | 120 | 6 |

### What the numbers say

**A foreign object costs about 120 ns to build and destroy, against 8.21 ns in
Go.** cgo's handle is one pointer and one allocation of 8 B; cppgo's is 205.7 ns
and **120 B in six allocations**. That is 1.7 times cgo's cost and 25 times the
Go type's.

**What the six allocations are.** The generated package owns the C++ storage
through a Go-side handle with a destructor closure: each construction allocates
the handle, the storage it pins for C++, and the closure `Close` runs, and the
destruction reads the destructor's message. cgo hands the object to C++ and
stores one pointer.

**The lifecycle is a per-object cost, not a per-call one.** A caller that
constructs one object and then calls it in a loop pays the lifecycle once and
the per-call cost from the scenario below; a caller that builds an object per
operation pays the 205.7 ns every time.

## 12. The object lifecycle: a method call on a live object

### The scenario

One `noexcept` method on a `bench::Counter` that is already alive, held by the
package for the whole run. Construction is out of the way, so the row is the
cost of a method call through the handle: the direct carrier with an implicit
`this` — the object the method was called on, which C++ passes as a hidden first
argument.

All three arms: the Go method, cgo's handle, and cppgo's generated object.

### The code

```cpp
// bench.hpp
class Counter {
public:
    std::int64_t get() const noexcept;
    // ...
};
```

```go
// gobase
sinkInt64 = existingCounter.Get()

// cgo — the direct call with a receiver
func (c *Counter) Get() int64 { return int64(C.bench_counter_get(c.ptr)) }

// cppgo — the generated signature
func (c *Counter) Get() int64
```

### The results

| Arm | Iterations | Runs | Median ns/op | Min–max ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `go_direct` | 732M–809M | 6 | 1.58 | 1.54–1.66 | 0 | 0 |
| `cgo` | 29.5M–34.4M | 6 | 38.0 | 36.6–39.5 | 0 | 0 |
| `cppgo` | 95.1M–100M | 6 | 11.9 | 11.2–12.9 | 0 | 0 |

### What the numbers say

**A method call on a live object costs the crossing and nothing more.** cppgo's
`Get` is 11.9 ns against the 10.5 ns of `cppgo_direct` on a free function: the
receiver adds about a nanosecond. cgo's is 38.0 ns against its own 29.6 ns
crossing. No arm allocates.

**The direct carrier wins here too, by 3.2 times**: 11.9 ns against 38.0 ns, the
same advantage the crossing shows. A method is not a more expensive call than a
free function across the boundary.

## What the scenarios add up to

- **The direct carrier is the cheapest way to call C++ from Go.** 10.5 ns
  against cgo's 29.6 ns on the same call, and it keeps its advantage on a method
  call (11.9 against 38.0) and on a successful status-returning call (20.5
  against 36.3).
- **A thunk costs about what cgo costs.** 29.2 ns against 29.6 ns on identical
  arithmetic: a declaration that has to report a status pays for the shape, not
  for cppgo.
- **Where the work dominates, the boundary stops mattering.** A string is 2.9
  times Go and a 1024-element vector 2.0 times, because the copy is most of the
  time.
- **By-value records allocate three times per call**, against cgo's one, on
  both the 16- and 24-byte rows: small in time, garbage in a loop.
- **The object lifecycle is a per-object cost.** 205.7 ns to construct and
  destroy an opaque object, against cgo's 119.8 ns and 8.21 ns in Go, while a
  method on a live one is 11.9 ns: an object reused is cheap, an object built
  per operation is not.
- **The callback carries the largest boundary overhead**: 5.5 times cgo and
  1200 B per call, from registering the Go function on every call.

## Related

- The suite, its raw output and how to run it: [`benchmarks/README.md`](https://github.com/Manu343726/cppgo/blob/main/benchmarks/README.md).
- Which construct gets which carrier: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- What each `cppgo` call becomes at the ABI: [Architecture]({{< ref "explanation/architecture.md" >}}).
