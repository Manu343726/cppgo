+++
title = "stats: bulk data"
description = "Arithmetic over a million-element []float64: (pointer, length) in both directions, a pointer-identity check that proves nothing was copied, and an int32_t status turned into a Go error."
weight = 20
+++

**Repository path:** [`examples/stats/`](https://github.com/Manu343726/cppgo/tree/main/examples/stats)

`stats` is the case a Go program actually calls out to C++ for: arithmetic over a
large `[]float64`. Every function takes its data as `(pointer, length)`, and the
ones that produce bulk output write it into a buffer the caller owns, so nothing
is copied or marshalled in either direction.

```console
$ cd examples/stats
$ make            # build libstats.so, generate, test, vet
$ make check      # prove the committed generated files are up to date
$ make bench      # C++ call against the pure-Go equivalent
```

## The header

Trimmed to the declarations (the file also carries the comments that explain
each one):

<span class="filename">examples/stats/include/stats/stats.hpp</span>

```cpp
namespace stats {

using std::size_t;

double mean(const double *xs, size_t n);

double stddev(const double *xs, size_t n);

size_t count_above(const double *xs, size_t n, double threshold);

// Returns the pointer it was given. If the slice had been copied on the way
// in, this would not be the address of the caller's first element.
const double *data_pointer(const double *xs, size_t n);

int32_t histogram(const double *xs, size_t n,
                  double *bins, size_t nbins,
                  double lo, double hi);

} // namespace stats

// The one deliberately extern "C" symbol in the library: a return register
// cannot carry a message, so the wrapper looks this up after a non-zero status.
extern "C" const char *cppgo_last_error();
```

Everything except `cppgo_last_error` is a normal C++ function defined out of line
in `src/stats.cpp`. The `extern "C"` accessor is the status convention's other
half: on failure, `histogram` returns non-zero and leaves a message for
`cppgo_last_error()`, which the generated wrapper turns into a Go `error`.

`data_pointer` looks pointless — it returns its argument. It is there to *prove*
something in a test: the pointer that arrives is the pointer that left, so the
slice was not copied.

## The directives

<span class="filename">examples/stats/stats.go</span>

```go
//cppgo:library libstats
//cppgo:header include/stats/stats.hpp

//cppgo:function stats::mean
func Mean(xs []float64) float64

//cppgo:function stats::stddev
func Stddev(xs []float64) float64

//cppgo:function stats::count_above
func CountAbove(xs []float64, threshold float64) int

//cppgo:function stats::data_pointer
func DataPointer(xs []float64) *float64

//cppgo:function stats::histogram
func Histogram(xs, bins []float64, lo, hi float64) error
```

What crosses is worth reading off the signatures: a `[]float64` is two integer
arguments (pointer, length), a `float64` is one argument in an XMM register, and
a function that declares **only** an `error` result is read as an integer
status. [Register assignment]({{< ref "manual/calling-convention.md" >}}) has the
full table and the exhaustion limit.

`Histogram` declares only `error`, and the C++ function returns `int32_t`, so the
integer is read as the **status**: `0` is `nil`, anything else becomes an error
whose message comes from `cppgo_last_error`. That is the direct ABI mode's only
error channel, and this example is the working version of it.

## The call site

From [`examples/stats/stats_test.go`](https://github.com/Manu343726/cppgo/blob/main/examples/stats/stats_test.go)
and the benchmark:

```go
if got := Mean([]float64{1, 2, 3, 4}); !close(got, 2.5) {
	t.Errorf("Mean = %v, want 2.5", got)
}

// The output crosses as (pointer, length) too: the counts go straight into
// the caller's slice.
bins := make([]float64, 16)
if err := Histogram(xs, bins, 0, 1); err != nil {
	t.Fatal(err)
}

// Same memory, not a copy.
if DataPointer(xs) != &xs[0] {
	t.Error("the slice was copied on the way in")
}
```

## What it teaches

- **(pointer, length) is the whole interface.** A million elements cost the same
  two registers as three. There is no marshalling step because there is no
  intermediate representation: C++ reads Go's backing array.
- **The direction is symmetric.** Input and output both cross as borrowed
  pointers; results are written where the caller can see them, not returned.
- **A pointer identity check is the proof.** `data_pointer` is the only way to
  demonstrate "not copied" from the outside, and it is worth having in a test
  when the whole point of the binding is throughput.
- **A status is not a value.** `histogram` returns `int32_t` and the Go binding
  declares only `error`, so the wrapper reads `EAX`, checks it, and calls
  `LastError` on failure. No out-parameter is needed for the status because the
  status *is* the return.
- **Empty slices are legal.** `Mean(nil)` is `(null, 0)` on the wire and the C++
  side returns 0, which the tests assert.

## The benchmark

`make bench` runs [`examples/stats/stats_bench_test.go`](https://github.com/Manu343726/cppgo/blob/main/examples/stats/stats_bench_test.go),
which compares each function against a pure-Go implementation over the same
memory. Two sizes matter and they answer different questions:

- **8 elements** — the cost is the crossing (the Asm trampoline plus the
  wrapper). Compare it with the project's 13.9 ns figure, and remember the clock
  caveat on [benchmarks]({{< ref "project/benchmarks.md" >}}).
- **a million elements** — the cost is memory bandwidth; the crossing is noise.
  That is precisely why the slice is passed as `(pointer, length)` and not
  copied.

This page does **not** quote the benchmark's results: they were not measured for
this page, and they depend on your CPU, compiler and Go version. Run it:

```console
$ cd examples/stats
$ make bench
```

Read the `cppgo` and `go` arms of each sub-benchmark. The difference at
`n=8` is the boundary; at `n=1048576` the two should be close, because the
arithmetic dominates. If they are not, the C++ loop is not doing what you think
— usually because it is not vectorisable as written.

Next: the [manual]({{< ref "manual/_index.md" >}}) for the rules behind all of
this.
