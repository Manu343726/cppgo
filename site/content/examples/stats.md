+++
title = "stats: bulk data"
description = "Arithmetic over a large []float64: (pointer, length) in both directions, a pointer-identity check that proves nothing was copied, and a 32-bit status turned into a Go error with -status."
weight = 20
+++

**Repository path:** [`examples/stats/`](https://github.com/Manu343726/cppgo/tree/main/examples/stats)

**Audience:** you want the shape a Go program actually calls out to C++ for —
arithmetic over a large `[]float64` — rather than one small class.

<div class="callout">
<span class="callout-title">This package is being regenerated</span>
<p>
The <code>examples/</code> directory on <code>main</code> still holds the v3
package. Issue #4 regenerates it against the v4 emitter. The command, the report
and the generated API on this page come from the v4 generator run against the
header below.
</p>
</div>

Every function takes its data as `(pointer, length)`, and the one that produces
bulk output writes into a buffer the caller owns, so nothing is copied or
marshalled in either direction.

## The header

Trimmed to the declarations (the file also carries the comments that explain
each one):

<span class="filename">examples/stats/include/stats/stats.hpp</span>

```cpp
namespace stats {

using std::int32_t;
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

Everything except `cppgo_last_error` is a normal C++ function defined out of
line in `src/stats.cpp`.

## Generate

The header declares `cppgo_last_error` itself, which is the library's own status
accessor. The generator would bind it as an ordinary function and collide with
the accessor the shim defines, so the invocation excludes it and names
`histogram` as a status function:

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Wl,-soname,libstats.so \
      -Iinclude -Iinclude/stats -o build/libstats.so src/stats.cpp
$ cppgo gen -header include/stats/stats.hpp -package stats \
      -library build/libstats.so -out ./stats \
      -status stats::histogram -exclude cppgo_last_error
stats::count_above(const double *, size_t, double) thunk  cppgo_thunk_9d4ab55408c1   not noexcept: an exception would unwind into Go
stats::data_pointer(const double *, size_t)  thunk  cppgo_thunk_f6b67dec6101    not noexcept: an exception would unwind into Go
stats::histogram(const double *, size_t, double *, size_t, double, double) thunk  cppgo_thunk_2f7a0df214c2   not noexcept: an exception would unwind into Go
stats::mean(const double *, size_t)          thunk  cppgo_thunk_a7638858c2ce    not noexcept: an exception would unwind into Go
stats::stddev(const double *, size_t)        thunk  cppgo_thunk_a0b464f80aef    not noexcept: an exception would unwind into Go
cppgo: built stats/libcppgo_stats.so with /usr/bin/g++
```

Every function is a thunk because none is `noexcept`: they can throw, so the
shim catches and the Go signature reports. The generated API is:

```go
func Open(path string) error
func CountAbove(xs []float64, threshold float64) (uint64, error)
func DataPointer(xs []float64) (*float64, error)
func Histogram(xs []float64, bins []float64, lo float64, hi float64) error
func Mean(xs []float64) (float64, error)
func Stddev(xs []float64) (float64, error)
```

`Histogram` returns only `error`, not `(int32, error)`, because `-status` named
it: the C++ `int32_t` is the failure channel, and 0 is `nil`.

## The call site

```go
xs := []float64{1, 2, 3, 4}

mean, err := stats.Mean(xs) // 2.5
if err != nil {
	log.Fatal(err)
}

bins := make([]float64, 16)
if err := stats.Histogram(xs, bins, 0, 5); err != nil {
	log.Fatal(err)
}

// Same memory, not a copy.
p, err := stats.DataPointer(xs)
if err != nil {
	log.Fatal(err)
}
if p != &xs[0] {
	log.Fatal("the slice was copied on the way in")
}
```

`DataPointer` looks pointless — it returns its argument. It is there to *prove*
something in a test: the pointer that arrives is the pointer that left, so the
slice was not copied.

The `Histogram` status is the other half: the shim copies the message the
library stored, and the generated wrapper turns a non-zero status into an error:

```
cppgo: stats::histogram: histogram: nbins must be positive
```

## What it teaches

- **(pointer, length) is the whole interface.** A million elements cost the same
  two registers as three. There is no marshalling step because there is no
  intermediate representation: C++ reads Go's backing array.
- **The direction is symmetric.** Input and output both cross as borrowed
  pointers; results are written where the caller can see them, not returned.
- **A pointer identity check is the proof.** `DataPointer` is the only way to
  demonstrate "not copied" from the outside, and it is worth having in a test
  when the whole point of the binding is throughput.
- **`-status` removes the integer.** `histogram` returns `int32_t` and the Go
  signature is `error` alone. The status *is* the return, so no out-parameter is
  needed for it.
- **Infrastructure is not API.** The library's own `cppgo_last_error` is
  excluded, not bound: it is the accessor the shim and the status convention
  use.

## The benchmark

The example's benchmark harness compares each function against a pure-Go
implementation over the same memory; it is part of the #4 regeneration. This
page does **not** quote its results: they were not measured for this page, and
they depend on your CPU, compiler and Go version.

Two sizes matter and answer different questions:

- **8 elements** — the cost is the crossing (the trampoline plus the wrapper).
  Compare it with the project's 15.9 ns figure, and remember the clock caveat on
  [benchmarks]({{< ref "project/benchmarks.md" >}}).
- **a million elements** — the cost is memory bandwidth; the crossing is noise.
  That is precisely why the slice is passed as `(pointer, length)` and not
  copied.

Next: the [manual]({{< ref "manual/_index.md" >}}) for the rules behind all of
this.
