# full — a complete Go + C++ project

A user's C++ library, its own CMake build, a cppgo-generated Go binding, and a
Go program with tests: the whole path from C++ headers to a running Go binary,
with no hand-written binding anywhere.

The example binds one small library, `metrics`. Every declaration in it shows
one thing the generator does: an opaque class, a POD by value, `std::string` and
`std::vector`, a call that throws, a call that reports a status code, a callback
from C++ into Go, and templates.

## The header

[`include/metrics/metrics.hpp`](include/metrics/metrics.hpp) is the input of the
whole example. Nothing in it mentions cppgo. These are the declarations
`cppgo gen` reads:

```cpp
namespace metrics {

struct Point {
    double x;
    double y;
};

// An opaque class: all of its state is private, and its size and layout stay
// on the C++ side. Go holds a pointer to an object C++ constructed.
//
// The state is the standard library's: a std::string and two std::vectors. The
// Go side never sees any of it: a container member is copied in and out by the
// generated accessors exactly like a container parameter.
class Histogram {
public:
    Histogram(std::string name, std::size_t buckets);
    Histogram(const Histogram &other);
    ~Histogram();

    std::size_t buckets() const noexcept;
    double total() const noexcept;
    static std::int32_t version() noexcept;

    void observe(double value);
    double mean() const;

    std::string name() const;
    std::vector<double> bounds() const;
    std::vector<std::size_t> counts() const;

private:
    std::string name_;
    std::vector<double> bounds_;
    std::vector<std::size_t> counts_;
    std::size_t total_;
    double sum_;
};

Point centroid(Point a, Point b) noexcept;

std::string describe(const std::string &prefix, const Point &p);

std::vector<double> scaled(const std::vector<double> &values, double factor);

double ratio(double numerator, double denominator);

std::int32_t validate_name(const std::string &name);

double apply(double (*fn)(double), double value);

template <typename T>
class Accumulator {
public:
    Accumulator() : total_() {}
    explicit Accumulator(T start) : total_(start) {}
    ~Accumulator() {}
    T total() const { return total_; }
    void add(T value) { total_ += value; }
    static T identity(T value) { return value; }

private:
    T total_;
};

template <typename T>
T twice(T value) {
    return value * 2;
}

} // namespace metrics
```

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

## Commands

```console
$ make          # CMake, cppgo gen, cppgo build -scan, go build, go test, vet
$ make run      # build the program and start it
$ make clean
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

Requirements: Go 1.25+, CMake 3.16+, a C++17 compiler, libclang 20+ (generation
only). The compiled program needs the two shared objects and no C++ toolchain.

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

## What is generated

```console
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

The report has one line per declaration. It names the carrier of each binding
and, for a template, the specializations the program uses:

```
metrics::Histogram::buckets() const noexcept           direct    _ZNK7metrics9Histogram7bucketsEv
metrics::Histogram::total() const noexcept             direct    _ZNK7metrics9Histogram5totalEv
metrics::Histogram::version() noexcept                 direct    _ZN7metrics9Histogram7versionEv
metrics::Histogram::observe(double)                    thunk     not noexcept
metrics::ratio(double, double)                         thunk     not noexcept
metrics::apply(double (*)(double), double)             reentrant callback
metrics::Accumulator<std::int32_t>                     tmpl      template class ::metrics::Accumulator<std::int32_t>;
metrics::Accumulator<double>                           tmpl      template class ::metrics::Accumulator<double>;
metrics::twice<std::int64_t>                           tmpl      template class ::metrics::twice<std::int64_t>;
```

The two commands are the two phases of the template flow:

1. `cppgo gen` writes the generic API. It compiles with no concrete type
   argument, which is why `gen/` can be generated before the program that uses
   `Accumulator[int32]` exists.
2. `cppgo build -scan ./cmd/...` loads the program, collects every
   instantiation it type-checks, writes one explicit instantiation and one
   thunk per member into the shim, and compiles it.

`-status metrics::validate_name` is the one convention the header cannot state:
`validate_name` returns a 32-bit status code, and the flag tells the generator
that a non-zero value is a failure rather than a value. The other declarations
need no configuration.

## The generated API

`cppgo gen` turned the header above into [`gen/zz_cppgo.gen.go`](gen/zz_cppgo.gen.go).
Its declarations are:

```go
func Open(path string) error
var CppgoLibraryPath string

type Point struct {
	X float64
	Y float64
}
func NewPoint() (*Point, error)
func (p *Point) Clone() (*Point, error)

type Histogram struct {
	obj *cppgo.Object
}
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

type Accumulator[T any] struct {
	obj *cppgo.Object
}
func NewAccumulator[T any](start T) (*Accumulator[T], error)
func NewAccumulatorDefault[T any]() (*Accumulator[T], error)
func (a *Accumulator[T]) Add(value T) error
func (a *Accumulator[T]) Total() (T, error)
func (a *Accumulator[T]) Close() error
func AccumulatorIdentity[T any](value T) (T, error)

func Twice[T any](arg0 T) (T, error)
```

Read the error results as a statement about the C++:

- `Buckets`, `Total`, `Version` and `Centroid` cannot fail, so they have no
  error result.
- `Observe` and `Mean` can throw, so they return an error.
- `ValidateName` reports a status code, so its integer is not in the Go
  signature at all: the call returns only an error.
- `Describe`, `Bounds`, `Counts` and `Scaled` cross `std::string` and
  `std::vector` values; the Go side sees `string`, `[]float64` and `[]uint64`.
- `Apply` takes a Go function, which C++ calls during the call.

## The call

A call from [`cmd/app/main.go`](cmd/app/main.go), where `Buckets` is a direct
binding, `Observe` and `Mean` go through the shim, and the error is C++'s:

```go
h, err := metrics.NewHistogram("latency", 4)
if err != nil {
	return err
}
defer h.Close()

for _, sample := range []float64{0.1, 0.6, 0.9} {
	if err := h.Observe(sample); err != nil {
		return err
	}
}

mean, err := h.Mean()
if err != nil {
	return err
}
fmt.Printf("%d buckets, mean %v\n", h.Buckets(), mean)
```

The callback runs a Go function inside the C++ call, and the generic API is
instantiated by the program itself:

```go
applied, err := metrics.Apply(func(v float64) float64 { return v * 3 }, 4)

acc, err := metrics.NewAccumulator[int32](10)
if err != nil {
	return err
}
defer acc.Close()
if err := acc.Add(5); err != nil {
	return err
}
sum, err := acc.Total()
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

## Loading the two shared objects

The generated package loads `gen/libcppgo_metrics.so` (the shim) and the user's
`build/libmetrics.so` (the library the shim was linked against). By default it
looks for the library beside the shim, under the soname the shim records
(`libmetrics.so.1`); this example's project keeps the two apart, so the program
and the tests set `CppgoLibraryPath`, and `CPPGO_LIBRARY_PATH` in the
environment overrides it. A deployment that copies `build/libmetrics.so.1`
beside the shim needs neither.

## Tests

`make test` runs `cmd/app/app_test.go` twice. What it asserts:

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

The first pass is `CGO_ENABLED=0`; the second adds the race detector. The
program and the tests use no cgo in either mode.

## What is not bound yet

The example stays inside what this build binds. Two things a real project may
expect are not here:

- `std::map`, `std::unordered_map`, `std::set`, `std::pair` and `std::tuple` as
  parameters and results are not part of the type map yet, so the example uses
  `std::string` and `std::vector`.
- The template flow is the two-phase one this example shows (`cppgo gen`, then
  `cppgo build -scan`). The generator does not yet take the given libraries as
  the first source of instantiations and fill the missing ones automatically, so
  the program's instantiations are collected by hand.
