+++
title = "Bulk data"
description = "Slices cross as (pointer, length). How to bind a routine over []float64, why a call per element is the wrong shape, and where the length has to sit in the C++ signature."
weight = 30
+++

A `[]float64` is contiguous memory. Crossing the boundary once and letting C++
loop over all of it is the shape that makes the FFI disappear; crossing once per
element is the shape that makes it dominate.

## The wrong shape first

Suppose `Vector` had an accessor:

```cpp
double Vector::at(std::size_t i) const;   // out of line
```

```go
//cppgo:method at
func (v Vector) At(i int64) float64
```

Calling it in a loop is correct, and it is what the Go type system invites:

```go
sum := 0.0
for i := 0; i < 10_000_000; i++ {
	sum += data.At(int64(i))
}
```

On the CPU described in [benchmarks]({{< ref "project/benchmarks.md" >}}) —
8-core x86-64, pinned to **964 MHz** — the trampoline measured **13.9 ns** per
call (~13 cycles; the nanoseconds are inflated by the clock, so treat them as a
frozen-CPU figure). Ten million calls at 13.9 ns is about **139 ms** of boundary
alone, before the C++ function does any work, and it also forces the loop to stay
scalar: each `at` is an opaque call the compiler cannot vectorise across.

The 13.9 ns is not the objection by itself — the objection is the shape. A
per-element API hands the C++ compiler one element at a time and forbids every
optimisation that needs to see the array.

## The right shape

Bind the routine, not the accessor. A dot product over two buffers:

<span class="filename">include/mathx/bulk.hpp</span>

```cpp
#pragma once

#include <cstddef>

namespace mathx {

// y[i] += alpha * x[i], for i in [0, min(x_len, y_len)).
void saxpy(double alpha,
           const double *x, std::size_t x_len,
           double *y, std::size_t y_len);

// sum(xs[i] * ys[i]) for i in [0, min(xs_len, ys_len)).
double dot(const double *xs, std::size_t xs_len,
           const double *ys, std::size_t ys_len);

} // namespace mathx
```

<span class="filename">src/bulk.cpp</span>

```cpp
#include "mathx/bulk.hpp"

namespace mathx {

void saxpy(double alpha, const double *x, std::size_t x_len,
           double *y, std::size_t y_len) {
    const std::size_t n = x_len < y_len ? x_len : y_len;
    for (std::size_t i = 0; i < n; ++i) {
        y[i] += alpha * x[i];
    }
}

double dot(const double *xs, std::size_t xs_len,
           const double *ys, std::size_t ys_len) {
    const std::size_t n = xs_len < ys_len ? xs_len : ys_len;
    double sum = 0.0;
    for (std::size_t i = 0; i < n; ++i) {
        sum += xs[i] * ys[i];
    }
    return sum;
}

} // namespace mathx
```

```go
//cppgo:function mathx::saxpy
func Saxpy(alpha float64, x, y []float64)

//cppgo:function mathx::dot
func Dot(xs, ys []float64) float64
```

Each Go slice spends **two** C++ parameters, at its own position: pointer, then
length. That is why `saxpy` takes `x, x_len, y, y_len` and not a single shared
`n` — the arguments are assigned to `RDI`, `RSI`, `RDX`, `RCX` in declaration
order, and a `double` takes an XMM slot instead of an integer one
([register assignment]({{< ref "manual/calling-convention.md" >}}) has the whole
table).

A C++ signature `saxpy(double, const double *, double *, std::size_t)` with one
shared `n` does **not** match: it has four parameters, the Go declaration
describes five, and the generator reports the arity mismatch. Pass the length
beside each pointer, or take `x []float64, n int64` and let the C++ side carry a
separate `size_t`.

## Call it

```go
x := make([]float64, 1<<20)
y := make([]float64, 1<<20)
// fill x ...

mathx.Saxpy(2.5, x, y)
fmt.Println(mathx.Dot(x, y))
```

One foreign call each. The C++ loop runs over the whole buffer, and the compiler
is free to vectorise it.

## The rules that come with a borrowed pointer

`Ptr` returns a pointer to the slice's backing array, and `nil` when the slice is
empty. The generated wrapper passes it with `uintptr(len(s))` and then calls
`KeepAlive`:

```go
cppgoRet := cppgo__Z... (cppgoSymSaxpy, alpha, cppgo.Ptr(x), uintptr(len(x)),
	cppgo.Ptr(y), uintptr(len(y)))
cppgo.KeepAlive(x, y)
```

- **The callee must not keep the pointer.** It is only valid for the duration of
  the call. A C++ function that stores the pointer in a member and reads it later
  is reading memory Go may have reused.
- **The caller owns the buffer.** C++ writes into it; it does not allocate or
  free it.
- **An empty slice is `(nil, 0)`.** That is a legal pointer/length pair, and the
  C++ side must handle `n == 0` without dereferencing. It also means you cannot
  distinguish an empty slice from a nil slice at the boundary — both are zero.
- **KeepAlive is not optional in hand-written code.** Generated wrappers emit it;
  if you write a binding yourself, call `cppgo.KeepAlive(s)` after the
  trampoline returns.

## Returning a buffer

A function cannot return a `[]float64`: the C++ side cannot allocate Go memory,
and the generated wrapper rejects a slice result. The two shapes that work are:

1. **write into a caller buffer** (`saxpy` above), or
2. **return a pointer and a length through out-parameters**, then copy in Go —
   which is only worth it when the C++ side already owns the storage.

For a result that Go must own, the first shape is almost always right: allocate
`make([]float64, n)` and pass it in.

## Strings are the same pair

A Go `string` crosses as `const char *` plus `size_t`, with the same rules:

```cpp
std::size_t count_zeros(const char *s, std::size_t n);
```

```go
//cppgo:function stats::count_zeros
func CountZeros(s string) int64
```

The bytes are **not** NUL-terminated. A C++ function that expects a C string
needs an explicit terminator in a `[]byte` you own, for example
`append(buf, 0)` and pass the slice.

## Measuring the difference

Do not take the arithmetic above as a benchmark of your workload: it isolates
the boundary, and the C++ loop's cost depends on what it does per element. Write
a Go benchmark and run it. The first example assumes the `At` accessor from the
top of this page, which the `Vector` tutorial deliberately does not bind; the
second is the shape you want.

```go
func BenchmarkAtLoop(b *testing.B) {
	v, _ := mathx.New(3, 4)
	defer v.Destroy()
	for i := 0; i < b.N; i++ {
		sum := 0.0
		for j := 0; j < 1024; j++ {
			sum += v.At(int64(j))
		}
		_ = sum
	}
}

func BenchmarkDot(b *testing.B) {
	xs := make([]float64, 1024)
	ys := make([]float64, 1024)
	for i := 0; i < b.N; i++ {
		_ = mathx.Dot(xs, ys)
	}
}
```

The gap you see will be the gap between "the compiler can vectorise" and "the
compiler cannot", and it will be much larger than 13.9 ns per 1024 elements. Run
it on your machine; the numbers in this site are one frozen CPU, and they are
there to show the order of magnitude of the crossing, not your throughput.

## Concurrency

Two goroutines may pass **different** buffers to the same function at the same
time: nothing is shared. Two goroutines writing the **same** buffer need the
same synchronisation they would need in C++, because cppgo adds none.

Next: [when the generator refuses]({{< ref
"tutorials/when-generation-refuses.md" >}}).
