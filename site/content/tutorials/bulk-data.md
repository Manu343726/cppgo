+++
title = "Bulk data"
description = "Slices cross as (pointer, length) without a copy; std::vector results are copied into a fresh Go slice. How to bind a routine over []float64, why a call per element is the wrong shape, and where the length has to sit in the C++ signature."
weight = 30
+++

A `[]float64` is contiguous memory. Crossing the boundary once and letting C++
loop over all of it is the shape that makes the FFI disappear. Crossing once per
element is the shape that makes it dominate.

**Audience:** you have a class working and now you need arrays, strings, or a
result that overflows one register.

## The wrong shape first

Suppose `Vector` had an accessor:

```cpp
double at(std::size_t i) const;   // out of line
```

```go
func (v *Vector) At(i uint64) (float64, error)
```

Calling it in a loop is correct, and it is what the Go type system invites:

```go
sum := 0.0
for i := 0; i < 10_000_000; i++ {
	n, err := data.At(uint64(i))
	if err != nil {
		return err
	}
	sum += n
}
```

On the machine in [benchmarks]({{< ref "project/benchmarks.md" >}}) a direct call
measured **15.9 ns** (2026-11-08, `go test -bench . -benchtime 200000x`). A thunk
adds a C++ frame on top. Ten million calls is boundary time spent before the
C++ function does any work, and it also forces the loop to stay scalar. Each
`At` is an opaque call the compiler cannot vectorise across.

The nanoseconds are not the objection. The objection is the shape. A per-element API hands the C++ compiler one element at a time and
forbids every optimisation that needs to see the array.

## The right shape. A borrowed slice

Bind the routine. The `mathx` fixture does both directions:

```cpp
// mathx::Vector — this is already in the header the tutorial uses.
double dot(const double *ys, std::size_t n) const;

// A free function over two buffers.
void saxpy(double a, const double *x, double *y, std::size_t n);
```

```go
func (v *Vector) Dot(ys []float64) (float64, error)
func Saxpy(a float64, x []float64, y []float64) error
```

Each Go slice becomes **two C++ parameters at its own position**: the pointer,
then the length. That is why `saxpy` takes `x, x_len` and `y, y_len`, the
arguments occupy `RDI`, `RSI`, `RDX`, `RCX` in declaration order, and a `double`
takes an XMM slot instead of an integer one ([register
assignment]({{< ref "manual/calling-convention.md" >}}) has the whole table).

The generated wrapper for `Saxpy` does one more thing. It checks that the slices
that share a length really do, because the C++ side takes one `n` for both:

```go
if len(x) != len(y) {
	return fmt.Errorf("cppgo: mathx::saxpy: the slices must have the same length, got %d and %d", len(x), len(y))
}
```

A C++ signature with one shared `n` and two pointers is a legitimate API. The
generated binding is where the invariant is enforced, because the C++ compiler
cannot see the second slice's Go length.

## Call it

```go
x := make([]float64, 1<<20)
y := make([]float64, 1<<20)
// fill x ...

if err := mathx.Saxpy(2.5, x, y); err != nil {
	log.Fatal(err)
}
fmt.Println(y[0], y[1])
```

One foreign call each. The C++ loop runs over the whole buffer, and the compiler
is free to vectorise it. Nothing was copied on the way in: C++ reads and writes
Go's backing array.

## The rules that come with a borrowed pointer

The wrapper passes `cppgo.Ptr(slice)` and `uint64(len(slice))`, then
`cppgo.KeepAlive(...)` after the call:

- **The callee must not keep the pointer.** It is valid only for the duration of
  the call. A C++ function that stores it in a member and reads it later is
  reading memory Go may have reused.
- **The caller owns the buffer.** C++ writes into it. Allocation stays on the
  Go side.
- **An empty slice is `(nil, 0)`.** That is a legal pair, and the C++ side must
  handle `n == 0` without dereferencing. It also means an empty slice and a nil
  slice are indistinguishable at the boundary.
- **The wrapper's `KeepAlive` is what keeps the memory reachable** until the call
  returns. A hand-written binding must do the same.

## Results beyond one register

Two shapes produce a new Go value:

- **`std::vector<T>`** is copied out into a fresh slice. The C++ object lives and
  dies inside the thunk. The Go slice owns its own memory:

  ```cpp
  std::vector<double> window(std::size_t n);
  ```

  ```go
  func Window(n uint64) ([]float64, error)
  ```

  Measured: `Window(4)` returns four values and `Window(0)` returns an empty,
  non-nil slice (`TestGeneratedPackageBuildsAndRuns`).

- **`std::string`** is copied in and out the same way:

  ```go
  func Describe(prefix string, value float64) (string, error)
  ```

The difference matters for the contract. A `[]float64` **parameter** is
borrowed. C++ sees Go's memory. A `[]float64` **result** is copied, and Go
owns the result. A function that must write into storage the caller already owns
should take a `double *` and a length instead of returning a `std::vector`.

## Measuring the difference

Do not take the arithmetic above as a benchmark of your workload. It isolates
the boundary, and the C++ loop's cost depends on what it does per element. The
first benchmark below assumes the `At` accessor from the top of this page, which
the generated `Vector` deliberately omits. The second binds the loop.
Write a Go benchmark and run it:

```go
func BenchmarkAtLoop(b *testing.B) {
	v, _ := mathx.NewVector(3, 4)
	defer v.Close()
	for i := 0; i < b.N; i++ {
		sum := 0.0
		for j := 0; j < 1024; j++ {
			n, _ := v.At(uint64(j))
			sum += n
		}
		_ = sum
	}
}

func BenchmarkDot(b *testing.B) {
	xs := make([]float64, 1024)
	for i := 0; i < b.N; i++ {
		_, _ = mathx.Dot(xs)
	}
}
```

The gap you see will be the gap between "the compiler can vectorise" and "the
compiler cannot", and it will be much larger than one trampoline per kilobyte.
Run it on your machine. The numbers on this site are one frozen CPU, and they
show the order of magnitude of the crossing.

## Concurrency

Two goroutines may pass **different** buffers to the same function at the same
time. Nothing is shared. Two goroutines writing the **same** buffer need the same
synchronisation they would need in C++, because cppgo adds none.

Next is [when generation refuses]({{< ref "tutorials/when-generation-refuses.md"
>}}).
