# stats — a large `[]float64`, in both directions

This is the case a Go program actually calls out to C++ for: arithmetic over a
big slice of numbers. Every function takes its data as `(pointer, length)` —
which is what a Go slice already is — and the result is written into a buffer
the caller owns, so nothing is copied or marshalled in either direction.

The header is the source of truth. `cppgo gen` reads it, decides per function
whether the call can go straight to the library or needs the shim, writes the Go
API and the assembly, and compiles the shim itself.

```console
$ cd examples/stats
$ make          # build libstats.so, generate, test (no cgo, then -race), vet
$ make bench    # the C++ calls against the pure-Go equivalents
```

## The header, and what the generator does with it

```cpp
namespace stats {

double mean(const double *xs, size_t n) noexcept;
double stddev(const double *xs, size_t n) noexcept;
size_t count_above(const double *xs, size_t n, double threshold) noexcept;
const double *data_pointer(const double *xs, size_t n) noexcept;
void histogram(const double *xs, size_t n, double *bins, size_t nbins,
               double lo, double hi);

} // namespace stats
```

The generated API is:

```go
func Open(path string) error

func Mean(xs []float64) float64
func Stddev(xs []float64) float64
func CountAbove(xs []float64, threshold float64) uint64
func DataPointer(xs []float64) *float64
func Histogram(xs []float64, bins []float64, lo float64, hi float64) error
```

`make gen` says why each one was carried the way it was:

```
stats::count_above(const double *, size_t, double) noexcept direct _ZN5stats11count_aboveEPKdmd  noexcept, not virtual, register class; the library exports the symbol
stats::data_pointer(const double *, size_t) noexcept        direct _ZN5stats12data_pointerEPKdm   noexcept, not virtual, register class; the library exports the symbol
stats::histogram(const double *, size_t, double *, size_t, double, double) thunk cppgo_thunk_2f7a…  not noexcept: an exception would unwind into Go
stats::mean(const double *, size_t) noexcept                direct _ZN5stats4meanEPKdm             noexcept, not virtual, register class; the library exports the symbol
stats::stddev(const double *, size_t) noexcept              direct _ZN5stats6stddevEPKdm           noexcept, not virtual, register class; the library exports the symbol
cppgo: built libcppgo_stats.so with /usr/bin/g++
```

Four functions are `noexcept` and defined out of line, so their mangled symbols
are in `libstats.so` and the trampoline calls them directly. `histogram` is not
`noexcept`, so it is a shim call. Two consequences are visible in the Go API:

- `Mean`, `Stddev`, `CountAbove` and `DataPointer` return **no error**. A call
  that cannot fail does not get an always-nil error in Go.
- `Histogram` returns **only** an `error`. It has no status return in C++ and
  none appears in Go.

## The binding, explained

### A slice spends two arguments

A Go slice is a pointer and a length, and that is exactly what the C++ side
declares: `Mean(xs []float64)` becomes `mean(const double *, size_t)`. The
pointer is borrowed for the duration of the call. A million elements cost the
same to hand over as three.

`data_pointer` returns the pointer it was given. `TestSliceIsNotCopied` checks
both halves of the claim:

```go
got := DataPointer(xs)
if got != &xs[0] {
	t.Fatalf("the C++ side saw %p, but the Go slice starts at %p: the slice was copied", got, &xs[0])
}

*got = 100
if xs[0] != 100 { /* not the same memory */ }
```

The pointer that arrived is the pointer that left, and writing through it
changes the Go slice, because it is Go memory. `TestHistogramWritesThroughTheSlice`
does the same for the output direction: the counts appear in the caller's
`bins`, and a second call starts from zero because the C++ `std::fill` cleared
that same array.

### The error channel is the C++ one

`histogram` reports a bad range by throwing `std::invalid_argument`:

```cpp
if (!(hi > lo)) {
    throw std::invalid_argument("histogram: hi must be greater than lo");
}
```

The shim wraps the call in `try`/`catch`, stores the exception's `what()` on the
calling thread and returns a status. The generated binding reads the message and
returns a `*cppgo.CallError`:

```go
err := Histogram([]float64{1, 2, 3}, make([]float64, 4), 8, 0)
var call *cppgo.CallError
errors.As(err, &call) // call.Symbol == "stats::histogram"
// err.Error() contains "histogram: hi must be greater than lo"
```

There is no `cppgo_last_error` accessor to write, and no status convention to
declare. The exception is the interface, and the shim is where it becomes a Go
error. `TestHistogramErrorPath` asserts both the text and the symbol.

## Measured numbers

`make bench` runs

```console
$ CGO_ENABLED=0 go test -run '^$' -bench . -benchmem .
```

with the library paths in the environment. One run:

| benchmark | cppgo | pure Go | ratio |
|---|---|---|---|
| `Mean`, 8 elements | 49.90 ns | 13.06 ns | 3.8x slower |
| `Mean`, 1 048 576 | 3.354 ms | 3.320 ms | 1.01x |
| `Stddev`, 1 048 576 | 6.682 ms | 6.715 ms | 1.00x |
| `Histogram`, 1 048 576 | 11.77 ms | 12.47 ms | 0.94x |
| `CountAbove`, 1 048 576 | 1.595 ms | 2.299 ms | 0.69x |
| `DataPointer` (the call itself) | 43.16 ns | 2.13 ns | 20x slower |

**Read the ratios, not the nanoseconds.** The absolute values are a property of
the clock the run happened at, of the machine's power profile and of whatever
else was running. This run was not taken at a pinned clock, so its nanoseconds
are not comparable with the numbers in the project's design document, which were
measured at 964.8 MHz with boost disabled. The ratios and the per-element cost
are what travel.

What the rows mean:

- **The crossing is a constant, and that is the point.** 43 ns is ~86% of the
  50 ns an 8-element `Mean` costs, and 0.001% of the 3.35 ms a million-element
  one costs. Per element at n = 1 048 576 it is 0.04 ns. That is the argument
  for the shape "one call over the slice": the boundary does not scale, the data
  does.
- **At a million elements the two arms are the same code in a different
  compiler.** `Mean` runs at ~3.2 ns per element in both arms, and `Stddev` at
  ~6.4 ns: those are the arithmetic, not the boundary. The million-element
  `Mean` gap is 1%, which is run-to-run noise.
- **`CountAbove` is ~1.4x faster on the C++ side**, and that is a compiler
  difference, not a boundary one: the C++ compiler vectorises the
  compare-and-count loop at `-O2`, the Go compiler does not. It is included
  precisely because it is the kind of thing that makes calling out to C++ worth
  it.
- **`Histogram` crosses through the shim and is still not slower.** The shim is
  one extra C++ frame; at a million elements the loop dominates it completely.
- **A call per element would be a disaster.** At 43 ns of crossing, one call per
  element over a million elements is 43 ms of boundary alone, several times the
  whole bulk call. The example binds the loop, not the accessor.

`B/op` and `allocs/op` are zero in every arm: a slice crossing the boundary
allocates nothing, in either direction.

The raw output of the run above:

```
BenchmarkMean/n=8/cppgo-16         	24334094	        49.90 ns/op	1282.53 MB/s	       0 B/op	       0 allocs/op
BenchmarkMean/n=8/go-16            	88661422	        13.06 ns/op	4899.58 MB/s	       0 B/op	       0 allocs/op
BenchmarkMean/n=1048576/cppgo-16   	     360	   3353696 ns/op	2501.30 MB/s	       0 B/op	       0 allocs/op
BenchmarkMean/n=1048576/go-16      	     358	   3320295 ns/op	2526.46 MB/s	       0 B/op	       0 allocs/op
BenchmarkStddev/cppgo-16           	     181	   6682132 ns/op	1255.38 MB/s	       0 B/op	       0 allocs/op
BenchmarkStddev/go-16              	     177	   6714782 ns/op	1249.27 MB/s	       0 B/op	       0 allocs/op
BenchmarkHistogram/cppgo-16        	      99	  11770692 ns/op	 712.67 MB/s	       0 B/op	       0 allocs/op
BenchmarkHistogram/go-16           	      98	  12466080 ns/op	 672.91 MB/s	       0 B/op	       0 allocs/op
BenchmarkCall/cppgo-16             	28220121	        43.16 ns/op	       0 B/op	       0 allocs/op
BenchmarkCall/go-16                	562008502	         2.128 ns/op	       0 B/op	       0 allocs/op
BenchmarkCountAbove/cppgo-16       	     746	   1594503 ns/op	5260.95 MB/s	       0 B/op	       0 allocs/op
BenchmarkCountAbove/go-16          	     513	   2299309 ns/op	3648.32 MB/s	       0 B/op	       0 allocs/op
```

## Layout

```
examples/stats/
├── include/stats/stats.hpp    declarations only
├── src/stats.cpp              the loops; compiled into the user's library
├── .clangd                    the project's flags for the parser
├── Makefile                   lib, gen, test, vet, check, bench, clean
├── stats_test.go              assertions, the pointer-identity check, Go reference impls
├── stats_bench_test.go        the benchmark, C++ arm and Go arm
├── zz_cppgo.gen.go            generated: the Go API, Open, the trampolines' declarations
├── zz_cppgo_amd64.s           generated: one NOSPLIT trampoline per binding
├── cppgo_shim/                generated: the shim's C++ sources
├── libcppgo_stats.so          built by cppgo gen (ignored, not committed)
├── go.mod                     its own module, replace ../..
└── build/                     ignored: libstats.so and the locally built cppgo
```

The generated sources are committed so the example can be read without running
the generator. `make gen` reproduces them, `make check` proves they are current,
and the compiled objects are ignored:

```console
$ make check
build/cppgo check -header include/stats/stats.hpp -package stats -library build/libstats.so -out .
.: generated files are up to date
```

The pure-Go implementations the tests and benchmarks compare against are in
[`stats_test.go`](stats_test.go) — `goMean`, `goStddev`, `goCountAbove`,
`goHistogram` — so the comparison is against code you can read, not a number in
this file.

## Where the shared objects come from at run time

The generated package loads `libstats.so` first and then `libcppgo_stats.so`.
The shim names the user's library in its `DT_NEEDED` entry by soname, which is
why the Makefile links with `-Wl,-soname,libstats.so`; loading the library first
is what makes that name resolve to the object the process already holds. The
test sets the two paths with `CppgoLibraryPath` and `Open`, and
`CPPGO_STATS_LIB` / `CPPGO_STATS_SHIM` are what the Makefile passes it.

## Requirements

- Go 1.25 or newer.
- A C++17 compiler (`make CXX=clang++` works too).
- libclang, which `cppgo gen` uses to parse the header: `sudo apt install clang
  libclang-dev`, or `sudo pacman -S clang`.

The Makefile builds its own `cppgo` from `../../cmd/cppgo` into `build/cppgo`, so
no global install is needed.

## Other targets

```console
$ make lib      # just the user's library
$ make gen      # just the binding (Go API, assembly, shim sources, shim .so)
$ make test     # go test, CGO_ENABLED=0 then -race
$ make vet      # go vet, including the generated assembly declarations
$ make check    # cppgo check: are the committed generated sources current?
$ make bench    # the benchmark table above
$ make clean
```
