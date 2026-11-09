# full — a complete Go + C++ project

A user's C++ library, its own CMake build, a cppgo-generated Go binding, and a
Go program with tests: the whole path from C++ headers to a running Go binary,
with no hand-written binding anywhere.

The example is the source material for the tutorial. It binds one small library,
`metrics`, and every construct in it exists to show one thing the generator does.

## Layout

```
examples/full/
├── CMakeLists.txt              the user's C++ build, and the compile database
├── include/metrics/metrics.hpp the C++ API; nothing in it mentions cppgo
├── src/metrics.cpp             the C++ implementation
├── Makefile                    CMake, cppgo gen/build, go build, go test
├── go.mod, go.sum              the Go module of the example
├── cmd/app/main.go             the program: it calls every binding
├── cmd/app/app_test.go         the tests, run in both CGO modes
└── gen/                        the generated Go package
    ├── doc.go                  hand-written package comment
    ├── zz_cppgo.gen.go         the Go API
    ├── zz_cppgo_amd64.s        one trampoline per direct binding
    └── cppgo_shim/             the C++ thunk sources
```

The generated sources and the shim sources are committed, because the example
is teaching material: `make` reproduces them, and `make check` fails when they
are stale. The compiled objects (`build/`, `gen/libcppgo_metrics.so`) are not.

## Requirements

| Tool | Version |
|---|---|
| Go | 1.25 or newer |
| CMake | 3.16 or newer |
| A C++ compiler | C++17; CMake picks it, `-DCMAKE_CXX_COMPILER` overrides it |
| libclang | 20 or newer, for `cppgo gen` and `cppgo check` |

libclang is needed to generate, not to run. The program needs the two shared
objects and no C++ toolchain at run time.

## Build and run

```console
$ make          # CMake, cppgo gen, cppgo build -scan, go build, go test
$ make run      # build the program and start it
$ make clean
```

`make run` prints one line per binding:

```console
metrics 4
histogram "latency": 4 buckets, 3 samples
  bounds: [0 0.25 0.5 0.75 1]
  counts: [1 0 1 1]
  mean:   0.5333333333333333
  clone:  4 samples after one more, original 3
centroid((0, 0), (2, 4)) = (1, 2)
describe = "corner(2.000000, 4.000000)"
scaled([1 2 3], 2) = [2 4 6]
ratio(9, 4) = 2.25
ratio(1, 0) failed as expected: cppgo: metrics::ratio: division by zero
validate_name("") failed as expected: cppgo: metrics::validate_name: the call failed without a message
apply(v*3, 4) = 12
Accumulator[int32](10) + 5 = 15
Accumulator[float64](1.5) + 0.25 = 1.75
twice[int64](21) = 42
```

Every target:

| Target | What it does |
|---|---|
| `make` | `app`, `test`, then `vet` |
| `make lib` | `cmake -S . -B build` and `cmake --build build`: `build/libmetrics.so` |
| `make gen` | `cppgo gen`: the generic Go API, the assembly, the shim sources and the shim |
| `make scan` | `cppgo build -scan ./cmd/...`: the program's template instantiations are added to the shim |
| `make app` | `go build ./cmd/app` into `build/app` |
| `make test` | `go test ./...` with `CGO_ENABLED=0`, then with `-race` |
| `make vet` | `go vet ./...` |
| `make check` | `cppgo check`: the committed generated sources are current |
| `make run` | build and start the program |

## The C++ build

`CMakeLists.txt` is the user's build and nothing else:

```cmake
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
add_library(metrics SHARED src/metrics.cpp)
target_include_directories(metrics PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)
set_target_properties(metrics PROPERTIES VERSION 1.0.0 SOVERSION 1)
```

No generated file is compiled into `libmetrics.so`, and cppgo never asks the
project to add a step. The one thing the project provides is
`build/compile_commands.json`: the database CMake writes because of
`CMAKE_EXPORT_COMPILE_COMMANDS`. cppgo reads the header with those flags, and
compiles its shim with them too, so the shim and the library agree on the
standard and the standard library.

## Generating the binding

```console
$ cd examples/full
$ cmake -S . -B build
$ cmake --build build
$ build/cppgo gen -header include/metrics/metrics.hpp -package metrics \
      -library build/libmetrics.so \
      -compile-commands build/compile_commands.json \
      -out ./gen -status metrics::validate_name
$ build/cppgo build -header include/metrics/metrics.hpp -package metrics \
      -library build/libmetrics.so \
      -compile-commands build/compile_commands.json \
      -out ./gen -status metrics::validate_name \
      -scan ./cmd/... -scan-dir .
```

The two commands are the two phases of the template flow:

1. `cppgo gen` writes the generic API. It compiles with no concrete type
   argument, which is why `gen/` can be generated before the program that uses
   `Accumulator[int32]` exists.
2. `cppgo build -scan ./cmd/...` loads the program, collects every
   instantiation it type-checks, writes one explicit instantiation and one
   thunk per member into the shim, and compiles it. The report names each
   specialization:

   ```
   metrics::Accumulator<double>       tmpl float64  template class ::metrics::Accumulator<double>;
   metrics::Accumulator<std::int32_t> tmpl int32    template class ::metrics::Accumulator<std::int32_t>;
   metrics::twice<std::int64_t>       tmpl int64    template class ::metrics::twice<std::int64_t>;
   ```

`-status metrics::validate_name` is the one convention the header cannot state:
`validate_name` returns a 32-bit status code, and the flag tells the generator
that a non-zero value is a failure rather than a value. The other declarations
need no configuration.

The report also says how each declaration is carried. The two accessors that
are `noexcept` and out of line are direct; everything else goes through the
generated shim, and `apply`, whose parameter is a C++ function pointer, uses the
callback-capable carrier:

```
metrics::Histogram::buckets() const noexcept          direct    _ZNK7metrics9Histogram7bucketsEv
metrics::Histogram::total() const noexcept            direct    _ZNK7metrics9Histogram5totalEv
metrics::Histogram::version() noexcept                direct    _ZN7metrics9Histogram7versionEv
metrics::Histogram::observe(double)                   thunk     not noexcept
metrics::ratio(double, double)                        thunk     not noexcept
metrics::apply(double (*)(double), double)            reentrant callback
```

## The generated API

```go
func Open(path string) error
var CppgoLibraryPath string

type Point struct {
    X float64
    Y float64
}
func NewPoint() (*Point, error)
func (p *Point) Clone() (*Point, error)

type Histogram struct{ /* unexported storage owned by C++ */ }
func NewHistogram(name string, buckets uint64) (*Histogram, error)
func (h *Histogram) Clone() (*Histogram, error)
func (h *Histogram) Close() error
func (h *Histogram) Buckets() uint64
func (h *Histogram) Total() float64
func (h *Histogram) Observe(value float64) error
func (h *Histogram) Mean() (float64, error)
func (h *Histogram) Name() (string, error)
func (h *Histogram) Bounds() ([]float64, error)
func (h *Histogram) Counts() ([]uint64, error)

func Centroid(a Point, b Point) Point
func Describe(prefix string, p *Point) (string, error)
func Scaled(values []float64, factor float64) ([]float64, error)
func Ratio(numerator float64, denominator float64) (float64, error)
func ValidateName(name string) error
func HistogramVersion() int32
func Apply(fn func(float64) float64, value float64) (float64, error)

type Accumulator[T any] struct{ /* unexported storage owned by C++ */ }
func NewAccumulator[T any](start T) (*Accumulator[T], error)
func NewAccumulatorDefault[T any]() (*Accumulator[T], error)
func (a *Accumulator[T]) Add(value T) error
func (a *Accumulator[T]) Total() (T, error)
func (a *Accumulator[T]) Close() error
func AccumulatorIdentity[T any](value T) (T, error)

func Twice[T any](arg0 T) (T, error)
```

Read the error results as a statement about the C++:

- `Buckets` and `Total` are `noexcept`, so they have no error result;
  `Centroid` is `noexcept` too.
- `Observe` and `Mean` can throw, so they return an error.
- `ValidateName` reports a status code, so its integer is not in the Go
  signature at all: the call returns only an error.
- `Describe`, `Bounds`, `Counts` and `Scaled` cross `std::string` and
  `std::vector` values; the Go side sees `string`, `[]float64` and `[]uint64`.
- `Apply` takes a Go function, which C++ calls during the call.

## The program and the tests

`cmd/app/main.go` is the program: it opens the binding and reports what every
call returned. `cmd/app/app_test.go` asserts the same values, and the cases a
report cannot show:

| Test | Assertion |
|---|---|
| `TestHistogramValues` | constructor, `Buckets`, `Total`, `Observe`, `Name`, `Bounds`, `Counts`, `Mean` |
| `TestCloneIsIndependent` | `Clone` goes through the copy constructor: the copy has its own state |
| `TestPointByValue` | a 16-byte POD in and out by value |
| `TestVectorCopiesInAndOut` | a slice into `std::vector` and the result into a fresh slice |
| `TestThrowingCall` | a C++ throw arrives as a `*cppgo.CallError` naming `metrics::ratio` |
| `TestStatusCall` | status 0 is nil, a non-zero status is an error naming `metrics::validate_name` |
| `TestCallback` | the Go function runs inside the C++ call |
| `TestGenericInstantiations` | the two `Accumulator` specializations and `Twice[int64]` |
| `TestRun` | the program itself runs to completion |

`make test` runs them twice, with `CGO_ENABLED=0` and with the race detector,
and the program uses no cgo in either mode.

## Loading the two shared objects

The generated package loads `gen/libcppgo_metrics.so` (the shim) and the user's
`build/libmetrics.so` (the library the shim was linked against). By default it
looks for the library beside the shim, under the soname the shim records
(`libmetrics.so.1`); this example's project keeps the two apart, so the program
and the tests set `CppgoLibraryPath`, and `CPPGO_LIBRARY_PATH` in the
environment overrides it. A deployment that copies `build/libmetrics.so.1`
beside the shim needs neither.

## What is not bound yet

The example stays inside what this build binds. Three things a real project may
expect are not here:

- `std::map`, `std::unordered_map`, `std::set`, `std::pair` and `std::tuple` as
  parameters and results are not part of the type map yet, so the example uses
  `std::string` and `std::vector`. Tracked in #43.
- The template flow is the two-phase one this example shows (`cppgo gen`, then
  `cppgo build -scan`). The corrected contract makes the library the first
  source of instantiations and fills the missing ones automatically, so a
  project will not pass `-scan` by hand. Tracked in #45.
- A class with a standard-library container as a member is refused by this
  build: it reports a nested type inside the container. The example keeps
  `Histogram`'s state behind a pimpl, which is the usual C++ shape anyway and
  is what `-out` binds.
