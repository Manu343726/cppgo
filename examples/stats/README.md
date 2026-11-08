# stats

Five functions over `double` arrays, all taking their data as
`(pointer, length)`. Nothing is copied in either direction, including the array
`histogram` writes into.

The example shows two things at once: what a header looks like when some
functions cannot fail and one can, and what passing a Go slice costs at the
boundary.

## Commands

```console
$ make        # gen, test, vet
$ make lib    # build/libstats.so, with the project's own flags and soname
$ make gen    # cppgo gen: the Go API, the C++ side, and the compiled C++ side
$ make test   # CGO_ENABLED=0, then CGO_ENABLED=1 -race
$ make check  # cppgo check: are the committed generated sources current?
$ make clean
```

Requirements: Go 1.25+, a C++17 compiler, libclang 20+ (generation only).

## The header

```cpp
namespace stats {

using std::size_t;

double mean(const double *xs, size_t n) noexcept;
double stddev(const double *xs, size_t n) noexcept;
size_t count_above(const double *xs, size_t n, double threshold) noexcept;
const double *data_pointer(const double *xs, size_t n) noexcept;

void histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo,
               double hi);

} // namespace stats
```

`histogram` is the only one that can fail, and it reports failure by throwing
`std::invalid_argument`. Everything else cannot fail.

## The report

```console
$ make gen
build/cppgo gen -header include/stats/stats.hpp -package stats -library build/libstats.so -out .
stats::count_above(const double *, size_t, double) noexcept direct _ZN5stats11count_aboveEPKdmd noexcept, not virtual, register class; the library exports the symbol
stats::data_pointer(const double *, size_t) noexcept direct _ZN5stats12data_pointerEPKdm noexcept, not virtual, register class; the library exports the symbol
stats::histogram(const double *, size_t, double *, size_t, double, double) thunk  cppgo_thunk_2f7a0df214c2     not noexcept: an exception would unwind into Go
stats::mean(const double *, size_t) noexcept direct _ZN5stats4meanEPKdm          noexcept, not virtual, register class; the library exports the symbol
stats::stddev(const double *, size_t) noexcept direct _ZN5stats6stddevEPKdm      noexcept, not virtual, register class; the library exports the symbol
cppgo: built libcppgo_stats.so with /usr/bin/g++
```

## The generated API

```go
func Open(path string) error
var CppgoLibraryPath string

func Mean(xs []float64) float64
func Stddev(xs []float64) float64
func CountAbove(xs []float64, threshold float64) uint64
func DataPointer(xs []float64) *float64
func Histogram(xs []float64, bins []float64, lo float64, hi float64) error
```

The four functions that cannot fail return their value with **no error result**.
`Histogram` returns only an error: it has no value to return and it can fail.

`DataPointer` returns the address it was given, which shows that the slice is not
copied on the way in.

## Tests

| Test | Assertion |
|---|---|
| `TestMean` | scalar cases, then a slice whose mean is checked exactly |
| `TestStddev` | the population standard deviation against a known value |
| `TestCountAbove` | the strict comparison at the threshold |
| `TestHistogramWritesThroughTheSlice` | the output array is filled in place |
| `TestHistogramErrorPath` | `std::invalid_argument` arrives as a `*cppgo.CallError` |
| `TestSliceIsNotCopied` | `DataPointer` returns the Go slice's own address |
| `TestLargeInput` | a million elements, compared against a Go computation |

## Benchmarks

```console
$ CGO_ENABLED=0 go test -run '^$' -bench . -benchmem .
```

`BenchmarkMean` runs the same arithmetic over the same memory, once behind the
boundary and once in Go, at 8 elements and at a million. At 8 the cost is the
crossing; at a million it is memory bandwidth, which is the point of passing
`(pointer, length)` instead of copying. `BenchmarkCall` measures the crossing
alone, `BenchmarkHistogram` the two-way slice case, and `BenchmarkCountAbove`
the integer-result shape.

The cost of a call, compared with cgo and with plain Go, is on the
[Performance page](https://manu343726.github.io/cppgo/explanation/benchmarks/).

## The committed generated sources

`zz_cppgo.gen.go`, `zz_cppgo_amd64.s` and `cppgo_shim/` are committed so a reader
can see what the generator wrote. `make check` proves they are current: it fails
when a committed file differs from what `make gen` writes.
