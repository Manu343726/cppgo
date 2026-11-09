---
title: "Bind a C++ template library"
description: "Generate the Go API for a C++ template, then collect the specializations your program uses, on the metrics example."
weight: 20
---

A C++ template is not a class. `Accumulator<T>` is a recipe for a family of
classes, and the compiler writes the real one — `Accumulator<int32_t>`,
`Accumulator<double>` — only where a program supplies the type argument. That
single fact decides how a template is bound: the Go API can be generated for
every type at once, but the C++ code that carries a call exists only for the
types a program actually asks for.

So a template library takes two commands. `cppgo gen` writes the generic Go API.
`cppgo build -scan` reads your Go program, collects the type arguments it uses,
and adds the matching C++ code before it compiles the shim — the C++ library
that cppgo builds for the package and the generated Go loads at run time. The
reason for the split is
[in Architecture]({{< ref "explanation/architecture.md" >}}#templates-why-two-phases);
this lesson runs both commands on a real library.

## What you will build

You will bind the two templates of the `metrics` library in
[`examples/full`](https://github.com/Manu343726/cppgo/tree/main/examples/full),
build that project, and run its program. The lesson ends with these three lines
among the program's output:

```console
Accumulator[int32](10) + 5 = 15
Accumulator[float64](1.5) + 0.25 = 1.75
twice[int64](21) = 42
```

Every command below is a target of the example's `Makefile`, so you can run the
lesson while you read it. You write no binding and change neither the C++ header
nor the C++ build.

## The scenario

`examples/full` is a complete Go + C++ project: a small C++ library called
`metrics` with its own CMake build, a Go program that calls every declaration in
the library, and the tests that prove the calls work. The library is one header
and one source file:

- [`include/metrics/metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp)
  is the API, and the input of the whole lesson;
- [`src/metrics.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/src/metrics.cpp)
  is the implementation;
- [`cmd/app/main.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/main.go)
  is the program.

Most of the header is not about templates: an opaque class, a POD, strings,
vectors, a throw, a status code and a callback. This lesson follows only its two
templates; the rest is the subject of
[Build a complete Go + C++ project]({{< ref "tutorials/full-project.md" >}}).

## Step 1 — Look at the C++ header

These are the two templates, copied from
[`metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp):

```cpp
// A class template. The generic Go API exists after `cppgo gen`; each
// specialization the Go program type-checks is instantiated in the shim by
// `cppgo build -scan`.
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

// A free function template, collected the same way.
template <typename T>
T twice(T value) {
    return value * 2;
}
```

Nothing in that header knows that cppgo exists: no annotations, no `extern "C"`
block, no binding declarations. `Accumulator<T>` keeps a running total in a
private field, and `T` is whatever type the program asks for. `twice<T>` is a
free function with the same kind of parameter.

The C++ compiler produces no code for either one until something supplies `T`.
That is called instantiating the template, and the class it produces —
`Accumulator<std::int32_t>`, say — is a specialization. That is why the
generator cannot write a complete binding on its own: at generation time there is
no `Accumulator<int32_t>` to call.

## Step 2 — Build the library the way the project already builds it

The C++ side is an ordinary shared library (`.so`, loaded at run time) with its
own CMake build. The lines that matter for what follows are in
[`CMakeLists.txt`](https://github.com/Manu343726/cppgo/blob/main/examples/full/CMakeLists.txt):

```cmake
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

add_library(metrics SHARED src/metrics.cpp)

target_include_directories(metrics PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)

set_target_properties(metrics PROPERTIES VERSION 1.0.0 SOVERSION 1)
```

`CMAKE_EXPORT_COMPILE_COMMANDS` makes CMake write
`build/compile_commands.json`, the compilation database: the file that records
the flags each source file is compiled with. Both cppgo commands below read it,
so the header is parsed and the generated C++ is compiled with the same flags the
library was built with. `VERSION` and `SOVERSION` record a soname — the name the
loader looks the library up by — which is how the generated package finds it at
run time.

The example's [`Makefile`](https://github.com/Manu343726/cppgo/blob/main/examples/full/Makefile)
wraps that build in one target:

```console
$ make lib
cmake -S . -B build
cmake --build build
```

No generated file is compiled into `libmetrics.so`, and cppgo is not part of
this command.

## Step 3 — Phase one: generate the generic Go API

```console
$ make gen
build/cppgo gen -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name
```

The last lines of its report are the two templates:

```
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
```

`tmpl` marks the declaration as a template, and the `-` sits where the other
lines carry a symbol: there is no concrete function to point at yet. The count is
the number of bindings the generic API declares for the template.

The Go package those lines produced declares Go generics, from
[`gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
// Accumulator is metrics::Accumulator<T>: the generic API. The concrete specializations are the ones
// the program instantiates; `cppgo build` collects them and emits the shim.
type Accumulator[T any] struct {
	obj *cppgo.Object
}

// NewAccumulator constructs a metrics::Accumulator<T>.
//
// C++: metrics::Accumulator::Accumulator<T>(T), two-phase template
func NewAccumulator[T any](start T) (*Accumulator[T], error)

// NewAccumulatorDefault constructs a metrics::Accumulator<T>.
//
// C++: metrics::Accumulator::Accumulator<T>(), two-phase template
func NewAccumulatorDefault[T any]() (*Accumulator[T], error)

// Add calls metrics::Accumulator::add.
//
// C++: metrics::Accumulator::add(T), two-phase template
func (a *Accumulator[T]) Add(value T) error

// Total calls metrics::Accumulator::total.
//
// C++: metrics::Accumulator::total() const, two-phase template
func (a *Accumulator[T]) Total() (T, error)

// Close destroys the Accumulator. It returns cppgo.ErrClosed when the receiver has no live object.
func (a *Accumulator[T]) Close() error

// AccumulatorIdentity calls metrics::Accumulator::identity.
//
// C++: metrics::Accumulator::identity(T), two-phase template
func AccumulatorIdentity[T any](value T) (T, error)

// Twice calls metrics::twice.
//
// C++: metrics::twice(T), two-phase template
func Twice[T any](arg0 T) (T, error)
```

This package compiles, and `go vet` passes on it, with no instantiation at all.
The type argument is still open, which is the point of phase one: the Go program
can be written, and reviewed, before anybody knows which C++ types it needs.

Two details of the mapping are worth noticing now:

- the static member `identity` became a package-level function,
  `AccumulatorIdentity`, because a static member has no object to hang on;
- every doc comment carries the C++ declaration it came from, so a generic Go
  signature traces back to its line of the header.

## Step 4 — The program chooses the type arguments

Phase two needs a program. This is the part of
[`main.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/main.go)
that uses the two templates:

```go
i, err := metrics.NewAccumulator[int32](10)
if err != nil {
    return err
}
defer i.Close()
if err := i.Add(5); err != nil {
    return err
}
sum, err := i.Total()
if err != nil {
    return err
}
fmt.Printf("Accumulator[int32](10) + 5 = %v\n", sum)

d, err := metrics.NewAccumulator[float64](1.5)
if err != nil {
    return err
}
defer d.Close()
if err := d.Add(0.25); err != nil {
    return err
}
sumF, err := d.Total()
if err != nil {
    return err
}
fmt.Printf("Accumulator[float64](1.5) + 0.25 = %v\n", sumF)

twice, err := metrics.Twice[int64](21)
if err != nil {
    return err
}
fmt.Printf("twice[int64](21) = %v\n", twice)
```

The program names three concrete types: `int32`, `float64` and `int64`. Those
three names are the entire input of the second phase. Everything else the
sections above produced is generic and stays that way.

## Step 5 — Phase two: collect the instantiations the program uses

```console
$ make scan
build/cppgo build -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name -scan ./cmd/... -scan-dir .
```

The new lines of the report are the collected types, each next to the C++
specialization it stands for:

```
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
metrics::Accumulator<double>                 tmpl   float64                      template class ::metrics::Accumulator<double>;
metrics::Accumulator<std::int32_t>           tmpl   int32                        template class ::metrics::Accumulator<std::int32_t>;
metrics::twice<std::int64_t>                 tmpl   int64                        template class ::metrics::twice<std::int64_t>;
```

`-scan ./cmd/...` loads the Go package that matches the pattern, finds every use
of the generic API, and maps each Go type argument to a C++ type: `int32` is
`std::int32_t`, `float64` is `double`, `int64` is `std::int64_t`. `-scan-dir`
says where the patterns resolve. `-scan` is repeatable, so a program split over
several packages passes one pattern per package.

With those types known, the command writes the explicit instantiations into the
shim and compiles it. The lines it added are visible in
[`gen/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/cppgo_shim/zz_cppgo_shim.cpp):

```cpp
// The specializations the Go program instantiates. cppgo build collected
// them from the program; adding one here is what makes its members exist.
template class ::metrics::Accumulator<double>;
template class ::metrics::Accumulator<std::int32_t>;
```

The free function template needs no such declaration: one thunk per collected
type is written for it — the generated C++ function that carries the call — and
`twice<std::int64_t>` gets the one the program uses.

The Go package did not change between the two phases. Only the C++ side grew.

## Step 6 — Run it

```console
$ make run
...
Accumulator[int32](10) + 5 = 15
Accumulator[float64](1.5) + 0.25 = 1.75
twice[int64](21) = 42
```

Each line is one instantiation. `NewAccumulator[int32]` reached the C++
constructor through the thunk the scan wrote for `Accumulator<std::int32_t>`;
`NewAccumulator[float64]` used the other one; `Twice[int64]` used the free
function's. Without phase two the program would still have compiled, and then
failed at run time: the C++ code behind those three lines did not exist.

In your own project the consequence is a habit: the day you write
`NewAccumulator[float32]`, the Go API already accepts it, and the C++ side does
not. Re-run the scan before you build the program.

## Step 7 — When the C++ side is missing an instantiation

A type that the program uses and the shim has no specialization for is not a
crash and not a silent zero. The call returns an error that names the C++ line
to add:

```
cppgo: metrics::Accumulator<float> has no C++ instantiation in the shim: add "template struct ::metrics::Accumulator<float>;" to the header and run `cppgo build -scan <package>`: cppgo: symbol "cppgo_tmpl_a39b2a940cf6": rtl: dlsym "cppgo_tmpl_a39b2a940cf6" in ".../gen/libcppgo_metrics.so": ...: undefined symbol: cppgo_tmpl_a39b2a940cf6
```

There are two ways to supply that line:

- add `template struct ::metrics::Accumulator<float>;` to the header and re-run
  `cppgo build -scan`;
- or pass `-instantiate 'metrics::Accumulator<float>'` to the same command,
  which is the same input. The flag exists for a header you would rather not
  edit: the specialization still has to be visible from the header.

The check is cheap and the message is exact, so a missing type costs one
regeneration, not a debugging session.

## Step 8 — Check it in CI

`cppgo check` accepts the same arguments as the build, `-scan` included, because
the generated C++ side depends on the program's instantiations. The example
bundles both:

```console
$ make check
build/cppgo check -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name -scan ./cmd/... -scan-dir .
./gen: generated files are up to date
```

It exits `0` when the committed generated sources match a fresh generation and
`1` when one is missing or stale.
[Gate a generated package in CI]({{< ref "how-to/ci.md" >}}) puts that in a
pipeline.

## What the generic API cannot bind yet

| Limit | What it means |
|---|---|
| Type arguments are scalars and pointers to scalars | `bool`, the sized integers, `float32`, `float64`, and pointers to them. `string` is not a type argument |
| Member function templates are refused | "the member function template X is not emitted by this build yet" — expose the concrete instantiation as a non-template member |
| Operators and conversion operators are refused | a template whose API is `operator+` cannot be bound yet; give it a named member |
| Inheritance is refused | a template that derives from a base is out of this build |
| A template with a dependent, unclassified field is refused | "not a type this build classifies" |

Those are the generics, operators and inheritance items of the wider generator
work, not a configuration you have missed. The
[limits page]({{< ref "explanation/limits.md" >}}) lists each one, and the Eigen
target slice, which the two-phase flow exists for, is designed and not shipped.

## What you just did

You generated the Go API for two C++ templates without a concrete type in sight,
let the program declare the three types it uses, and had the second command turn
those three names into C++ specializations. No Go file was regenerated when the
C++ side grew, and no binding was written by hand.

## Related

- Every generated shape, including a template: [Generated API]({{< ref "reference/generated-api.md" >}}#templates).
- The command and its flags: [Command line]({{< ref "reference/cli.md" >}}).
- The same project end to end: [Build a complete Go + C++ project]({{< ref "tutorials/full-project.md" >}}).
