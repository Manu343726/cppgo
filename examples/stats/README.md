# stats — a large `[]float64`, in both directions

This is the case a Go program actually calls out to C++ for: arithmetic over a
big slice of numbers. Every function takes its data as `(pointer, length)` —
which is what a Go slice already is — and the results are written into a buffer
the caller owns, so nothing is copied or marshalled in either direction.

```console
$ cd examples/stats
$ make          # build libstats.so, generate, test, vet
$ make bench    # the C++ calls against the pure-Go equivalents
```

## What this example teaches

| what | where |
|---|---|
| a slice crossing as `(pointer, length)`, no copy | `Mean`, `Stddev`, `CountAbove` |
| the same for an **output** slice | `Histogram` writes into the caller's bins |
| proof that nothing was copied | `data_pointer` returns the address it was given |
| a direct C++ call that can fail | `histogram` returns an `int32_t` status, read as `error` |
| what a call actually costs, measured | `make bench`, numbers below |

The pure-Go implementations the tests and benchmarks compare against are in
[`stats_test.go`](stats_test.go) — `goMean`, `goStddev`, `goCountAbove`,
`goHistogram` — so the comparison is against code you can read, not a number in
this file.

## Layout

```
examples/stats/
├── include/stats/stats.hpp    declarations only, defined out of line
├── src/stats.cpp              the loops, and the cppgo_last_error accessor
├── stats.go                   the //cppgo: directives (rewritten in place by gen)
├── stats_test.go              assertions, the pointer-identity check, Go reference impls
├── stats_bench_test.go        the benchmark, C++ arm and Go arm
├── zz_cppgo.gen.go            generated: wrappers, lazy binding, Go side of the ABI
├── zz_cppgo_amd64.s           generated: one trampoline per symbol
├── Makefile                   builds the .so, runs gen, test, vet and bench
├── go.mod                     its own module, replace ../..
└── build/                     ignored: libstats.so, the cppgo binary, the CDB
```

## The binding, in full

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

A Go slice spends **two** C++ parameters — pointer, then length — at its own
position in the declaration. That is why `histogram` is

```cpp
int32_t histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo, double hi);
```

and not six arguments in some other order. The registers follow the declaration:

| call | integer registers | float registers | result |
|---|---|---|---|
| `Mean(xs)` | `RDI=&xs[0]`, `RSI=len(xs)` | — | `XMM0` |
| `CountAbove(xs, t)` | `RDI`, `RSI` | `XMM0=t` | `RAX` |
| `Histogram(xs, bins, lo, hi)` | `RDI`, `RSI`, `RDX=&bins[0]`, `RCX` | `XMM0=lo`, `XMM1=hi` | `EAX` (status) |
| `DataPointer(xs)` | `RDI`, `RSI` | — | `RAX` |

Note the `size_t` in the header and the `using std::size_t;` above it: the
generator reports a result type as it is spelled in the source, and
`std::size_t` in a *result* position trips the same rule that refuses
`std::string`. In a parameter position it is the length half of a `(pointer,
length)` pair and is fine either way.

## The error channel

A direct C++ call has no shim to catch an exception, so failure has to be an
ordinary return value. `histogram` returns `int32_t`:

```cpp
if (!(hi > lo)) {
    return fail("histogram: hi must be greater than lo");
}
```

and the Go binding declares **only** an `error`, which is what tells the
generator to read that integer as a status:

```go
//cppgo:function stats::histogram
func Histogram(xs, bins []float64, lo, hi float64) error
```

The wrapper it emits is worth reading:

```go
cppgoStatus := cppgo__ZN5stats9histogramEPKdmPdmdd(cppgoSymHistogram, …)
cppgo.KeepAlive(xs, bins)
if cppgoStatus != 0 {
    if err := cppgo.LastError(cppgoLib); err != nil {
        return fmt.Errorf("stats::histogram: %w", err)
    }
    return fmt.Errorf("stats::histogram: call failed with status %d", cppgoStatus)
}
return nil
```

`cppgo.LastError` looks up `cppgo_last_error` in the library and copies the
message out. That accessor is the one symbol in this example that is deliberately
`extern "C"` — a return register cannot carry a message — and it is optional: a
library that never fails does not need one. `TestHistogramErrorPath` asserts both
the message and the `stats::histogram` prefix the wrapper adds.

## "No copy", proved rather than claimed

`data_pointer` returns the pointer it was given. The test then checks both halves
of the claim:

```go
got := DataPointer(xs)
if got != &xs[0] {
	t.Fatalf("the C++ side saw %p, but the Go slice starts at %p: the slice was copied", got, &xs[0])
}

*got = 100
if xs[0] != 100 {
	t.Fatalf("writing through the returned pointer left xs[0] = %v: not the same memory", xs[0])
}
```

The pointer that arrived is the pointer that left, and writing through it changes
the Go slice, because it is Go memory. `TestHistogramWritesThroughTheSlice` does
the same for the output direction: the counts appear in the caller's `bins`, and
a second call starts from zero because the C++ `std::fill` cleared that same
array.

## Measured numbers

`make bench` runs

```console
$ go test -run '^$' -bench . -benchmem ./...
```

with the library path in the environment. Median of three runs on this machine — a
Ryzen 7 6800HS, **on battery in the `power-saver` profile, boost off, CPU pinned
at 964.8 MHz**:

| benchmark | cppgo | pure Go | ratio | per element (cppgo) |
|---|---|---|---|---|
| `Mean`, 8 elements | 44 ns | 21 ns | **2.1x slower** | — |
| `Mean`, 1 048 576 | 3.40 ms | 3.48 ms | 0.98x | 3.1 cycles |
| `Stddev`, 1 048 576 | 6.87 ms | 7.14 ms | 0.96x | 6.3 cycles |
| `Histogram`, 1 048 576 | 11.9 ms | 12.1 ms | 0.98x | 10.9 cycles |
| `CountAbove`, 1 048 576 | 1.63 ms | 2.34 ms | 0.69x | 1.5 cycles |
| `DataPointer` (the call itself) | 35.0 ns | 2.2 ns | 15.7x | — |

This is a laptop on battery, and later runs taken while three other builds were
in progress on the same machine read 51–69 ns for the 8-element case and 37–56 ns
for the bare call. The absolute numbers move with the load; the **ratio** between
the million-element arms and the fact that the crossing is a fixed ~35 ns did not.
Run it on an idle machine, or compare cycles rather than nanoseconds.

**Read the ratios, not the nanoseconds.** The clock is pinned at 964.8 MHz, so
everything absolute is inflated by roughly 3.3x against the same code on a
boosting core; the cycle column is derived from the ns at 0.9648 GHz and is the
number that travels.

What the rows mean:

- **The crossing is a constant, and that is the whole point.** 35 ns is ~80% of
  the 44 ns an 8-element `Mean` costs, and 0.001% of the 3.40 ms a million-element
  one costs. Per element at n = 1 048 576 it is 0.03 ns. That is why the shape is
  "one call over the slice": the boundary does not scale, the data does.
- **At a million elements the two arms are the same code in a different
  compiler.** Both means run at ~3.1 cycles per element, which is the latency of
  a serial `double` add dependency chain, not memory bandwidth: 8 MiB in 3.4 ms
  is only ~2.5 GB/s, and the loop is bound by the three-cycle FP add, not by
  DRAM. `Histogram` is ~11 cycles per element in both, which is the `divsd`
  latency. Neither gap is a cppgo gap.
- **`CountAbove` is 1.45x faster on the C++ side**, and that is a compiler
  difference, not a boundary one: g++ 16 at `-O2` vectorises the
  compare-and-count loop, the Go compiler does not. It is included precisely
  because it is the kind of thing that makes calling out to C++ worth it — and
  because the boundary is nowhere in the 1.5-vs-2.2 cycles gap.
- **A call per element would be a disaster.** Rebuild the math with the 35 ns
  figure: one call per element over a million elements is 35 ms of boundary alone,
  ten times the whole bulk call. That is the argument for the shape this example
  binds, and the site's [bulk data](https://manu343726.github.io/cppgo/tutorials/bulk-data/)
  tutorial makes it in full.

Where the 35 ns goes: the repository's own benchmark puts the bare trampoline at
~14 ns (13.9 ns / ~13 cycles at the same clock). The rest is the generated
wrapper around it — the lazy `dlopen` check, `ProbeStack()`, the argument
marshalling and the `KeepAlive`. A constant per call, not per element.

`make bench` reproduces all of it; the arms are labelled `cppgo` and `go`, and
`SetBytes` is set inside each arm so the throughput column is populated. This is
the run taken while the machine was busy — which is why the small-element rows
read higher than the table above, and why it is worth running on an idle box:

```console
$ make bench
BenchmarkMean/n=8/cppgo-16         	16936789	        69.48 ns/op	 921.19 MB/s	       0 B/op	       0 allocs/op
BenchmarkMean/n=8/go-16            	35118909	        29.64 ns/op	2159.58 MB/s	       0 B/op	       0 allocs/op
BenchmarkMean/n=1048576/cppgo-16   	     346	   3407476 ns/op	2461.82 MB/s	       0 B/op	       0 allocs/op
BenchmarkMean/n=1048576/go-16      	     351	   3442597 ns/op	2436.71 MB/s	       0 B/op	       0 allocs/op
BenchmarkStddev/cppgo-16           	     175	   6834428 ns/op	1227.40 MB/s	       0 B/op	       0 allocs/op
BenchmarkStddev/go-16              	     153	   7819489 ns/op	1072.78 MB/s	       0 B/op	       0 allocs/op
BenchmarkHistogram/cppgo-16        	      98	  12285650 ns/op	 682.80 MB/s	       0 B/op	       0 allocs/op
BenchmarkHistogram/go-16           	      90	  13285082 ns/op	 631.43 MB/s	       0 B/op	       0 allocs/op
BenchmarkCall/cppgo-16             	20035719	        56.41 ns/op	       0 B/op	       0 allocs/op
BenchmarkCall/go-16                	342561806	         3.153 ns/op	       0 B/op	       0 allocs/op
BenchmarkCountAbove/cppgo-16       	     555	   2451902 ns/op	3421.27 MB/s	       0 B/op	       0 allocs/op
BenchmarkCountAbove/go-16          	     261	   5478591 ns/op	1531.16 MB/s	       0 B/op	       0 allocs/op
```

The `go` arm of `CountAbove` at 5.5 ms in that listing, against 2.3 ms in the
clean run, is what contention looks like: it hits whichever arm is running when
the other work lands.

`B/op` and `allocs/op` being zero is part of the result: a slice crossing the
boundary allocates nothing, in either direction.

## Requirements

- Go 1.25 or newer.
- A C++17 compiler (`make CXX=clang++` works too).
- libclang, which `cppgo gen` uses to parse the header: `sudo apt install clang
  libclang-dev`, or `sudo pacman -S clang`.

The Makefile builds its own `cppgo` from `../../cmd/cppgo` into `build/cppgo`, so
no global install is needed.

## Other targets

```console
$ make test     # go test only (builds the library and regenerates first)
$ make vet      # go vet, including the generated assembly declarations
$ make check    # cppgo check: are the committed generated files up to date?
$ make clean
```

The test binary reads `CPPGO_STATS_LIB` if it is set (the Makefile sets it) and
otherwise looks for `build/libstats.so` relative to the package directory — the
loader does not search the current directory, and `//cppgo:library libstats` is
just the name the bindings would use in a real deployment.
