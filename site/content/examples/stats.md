+++
title = "stats: bulk data"
description = "Arithmetic over a large []float64: (pointer, length) in both directions, four direct calls and one throwing function, a pointer-identity check that proves nothing was copied, and an exception that reaches Go as an error."
weight = 20
+++

**Repository path:** [`examples/stats/`](https://github.com/Manu343726/cppgo/tree/main/examples/stats)

**Audience:** you want the shape a Go program actually calls out to C++ for. This is arithmetic over a large `[]float64`, with most of the work on the direct carrier.

Every function takes its data as `(pointer, length)`, and the one that produces
bulk output writes into a buffer the caller owns. Nothing is copied or
marshalled in either direction.

## The header

Trimmed to the declarations (the file also carries the comments that explain
each one):

<span class="filename">examples/stats/include/stats/stats.hpp</span>

```cpp
namespace stats {

using std::size_t;

double mean(const double *xs, size_t n) noexcept;
double stddev(const double *xs, size_t n) noexcept;
size_t count_above(const double *xs, size_t n, double threshold) noexcept;
const double *data_pointer(const double *xs, size_t n) noexcept;

// Throws std::invalid_argument for an empty bin range or a non-positive count.
void histogram(const double *xs, size_t n,
               double *bins, size_t nbins,
               double lo, double hi);

} // namespace stats
```

The split is the lesson. Four functions are `noexcept` and defined out of line. The direct carrier takes them, and their Go signatures have no error.
`histogram` can throw. It goes through the shim, and its Go signature is an error.

`data_pointer` returns the pointer it was given. It is there to *prove* something in a test. The pointer that arrives is the pointer that left. The slice was not copied.

## Generate

```console
$ make gen
build/cppgo gen -header include/stats/stats.hpp -package stats \
    -library build/libstats.so -out .
stats::count_above(const double *, size_t, double) noexcept direct _ZN5stats11count_aboveEPKdmd noexcept, not virtual, register class; the library exports the symbol
stats::data_pointer(const double *, size_t) noexcept        direct _ZN5stats12data_pointerEPKdm   noexcept, not virtual, register class; the library exports the symbol
stats::histogram(const double *, size_t, double *, size_t, double, double) thunk  cppgo_thunk_2f7a0df214c2     not noexcept: an exception would unwind into Go
stats::mean(const double *, size_t) noexcept                direct _ZN5stats4meanEPKdm             noexcept, not virtual, register class; the library exports the symbol
stats::stddev(const double *, size_t) noexcept              direct _ZN5stats6stddevEPKdm           noexcept, not virtual, register class; the library exports the symbol
cppgo: built libcppgo_stats.so with /usr/bin/g++
```

Four `direct` rows, one `thunk`. The generated API follows:

```go
func Open(path string) error
func CountAbove(xs []float64, threshold float64) uint64
func DataPointer(xs []float64) *float64
func Histogram(xs []float64, bins []float64, lo float64, hi float64) error
func Mean(xs []float64) float64
func Stddev(xs []float64) float64
```

`Mean`, `Stddev`, `CountAbove` and `DataPointer` have no error because the C++
declarations are `noexcept`. `Histogram` returns only an error because it has no
value to return and the shim catches what it throws.

## The call site

From the example's tests:

```go
xs := []float64{2, 4, 4, 4, 5, 5, 7, 9}

mean := stats.Mean(xs)        // 5
deviation := stats.Stddev(xs) // 2

counts := []float64{1, 5, 2, 5, 3}
above := stats.CountAbove(counts, 2) // 3

bins := make([]float64, 16)
if err := stats.Histogram(xs, bins, 0, 10); err != nil {
	log.Fatal(err)
}

// Same memory, not a copy.
p := stats.DataPointer(xs)
if p != &xs[0] {
	log.Fatal("the slice was copied on the way in")
}
```

A failing `Histogram` carries the exception's text:

```go
err := stats.Histogram(xs, nil, 0, 10)
// cppgo: stats::histogram: histogram: nbins must be positive
// errors.As(err, &callErr) → *cppgo.CallError{Symbol: "stats::histogram", ...}
```

## What it teaches

- **(pointer, length) is the whole interface.** A million elements cost the same
  two registers as three. There is no marshalling step because there is no
  intermediate representation: C++ reads Go's backing array.
- **The direction is symmetric.** Input and output both cross as borrowed
  pointers. Results are written where the caller can see them.
- **A pointer identity check is the proof.** `DataPointer` is the only way to
  demonstrate "not copied" from the outside, and it is worth having in a test
  when the whole point of the binding is throughput.
- **The carrier is visible in the signature.** Four `noexcept` calls return a
  value and no error. The one throwing call returns an error and no value.
- **Infrastructure stays out of the API.** This example has no status accessor
  to exclude. Its failures are exceptions, and the shim is the C++ frame that
  catches them.

## The benchmark

`make bench` compares each function against a pure-Go implementation over the
same memory. This page does **not** quote its results. They are run-to-run measurements on one machine, and the example's own README records the run it took. Two sizes matter and answer different questions:

- **8 elements** — the cost is the crossing (the trampoline plus the wrapper).
  Compare it with the project's 15.9 ns figure, and remember the clock caveat on
  [benchmarks]({{< ref "project/benchmarks.md" >}}).
- **a million elements** — the cost is memory bandwidth. The crossing is noise.
  That is why the slice is passed as `(pointer, length)`.

Next is the [manual]({{< ref "manual/_index.md" >}}) for the rules behind all
of this.
