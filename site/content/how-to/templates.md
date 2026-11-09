---
title: "Bind a C++ template library"
description: "Generate the Go API for a C++ template, then collect the specializations your program uses, on the metrics example."
weight: 20
---

A C++ template is not a class. `Accumulator<T>` is a recipe for a family of
classes: `T` is a *template parameter*, a placeholder for a type, and the
compiler writes the real class — `Accumulator<int32_t>`, `Accumulator<double>` —
only where a program supplies the type to put in its place. The type supplied is
the *type argument*, and the class it produces is an *instantiation*. That single
fact decides how a template is bound. To bind one is to generate the Go package
that calls it: the Go API can be generated for every type at once, but the C++
code that carries a call exists only for the types a program actually asks for.

So a template library takes two commands. `cppgo gen` writes the generic Go API.
`cppgo build -scan` loads and type-checks your Go program with Go's own package
loader, collects the type arguments it passes to that API, and adds the matching
C++ code before it compiles the shim — the C++ library that cppgo builds for the
package and the generated Go loads at run time. The reason for the split is
[in Architecture]({{< ref "explanation/architecture.md" >}}#templates-why-two-phases);
this guide runs both commands on a real library.

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

This is the C++ that block uses, line by line. The two comments above the
declarations are the example's own notes on the flow: they name the two phases,
and every term in them is defined here or below.

- `template <typename T>` turns the declaration that follows into a template and
  names its parameter `T`. `typename` says `T` stands for a type — a class, a
  built-in type such as `double`, anything — rather than for a value.
- `class Accumulator { … };` declares the class, and the semicolon after the
  closing brace is required in C++.
- `Accumulator() : total_() {}` is a *default constructor*: it creates an object
  with no argument. The `: total_()` between the parameter list and the body is
  the *member initializer list* — the place a constructor says how each field
  starts, here zero-initialized. `{}` is the empty body.
- `explicit Accumulator(T start) : total_(start) {}` is a second constructor.
  `explicit` forbids the compiler from using it to convert a bare `T` into an
  `Accumulator` implicitly, so a call site has to say `Accumulator(...)`. The Go
  API gets one constructor per C++ constructor.
- `~Accumulator() {}` is the *destructor*, the function C++ runs when the object
  is destroyed; the body is empty because there is nothing for it to do — the
  field's own destructor runs after it.
- `T total() const { return total_; }` is a method with the return type in front.
  `const` after the parameter list promises it does not modify the object, and
  `total_` is the private field.
- `void add(T value) { total_ += value; }` returns nothing (`void`) and adds to
  the field. `+=` is Go's `+=`.
- `static T identity(T value) { return value; }` is a *static* member: it belongs
  to the class but needs no object. A static member becomes a package-level Go
  function.
- `private:` introduces the state, `T total_;` is the field itself.
- The second block is a *free function template*: a function, not a class member,
  with the same kind of parameter.

Nothing in that header is written *for* cppgo: no annotations, no `extern "C"`
block — the marker that gives a function C linkage, so that its compiled name is
the one written instead of a mangled one, and the reason generated thunks carry
it — and no binding declarations. The comments explain what each declaration
teaches; they are not something the generator reads. `Accumulator<T>` keeps a
running total in a private field, and `T` is whatever type the program asks for.
`twice<T>` is a free function with the same kind of parameter.

The C++ compiler produces no code for either one until something supplies `T`.
That is called instantiating the template, and the class it produces —
`Accumulator<std::int32_t>`, say — is a specialization. (`std::int32_t` is C++'s
fixed-width 32-bit signed integer, the type Go spells `int32`.) That is why the
generator cannot write a complete binding on its own: at generation time there is
no `Accumulator<int32_t>` to call.

## Step 2 — Build the library the way the project already builds it

The C++ side is an ordinary shared library (`.so`, loaded at run time) with its
own CMake build. CMake is a build-system generator: it reads `CMakeLists.txt` and
writes the makefiles that compile the sources. The lines that matter for what
follows are in
[`CMakeLists.txt`](https://github.com/Manu343726/cppgo/blob/main/examples/full/CMakeLists.txt):

```cmake
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

add_library(metrics SHARED src/metrics.cpp)

target_include_directories(metrics PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)

set_target_properties(metrics PROPERTIES VERSION 1.0.0 SOVERSION 1)
```

- `CMAKE_CXX_STANDARD 17` compiles as C++17; the flag matters to cppgo because
  the header is parsed with the same standard it is built with.
- `CMAKE_EXPORT_COMPILE_COMMANDS ON` makes CMake write
  `build/compile_commands.json`, the *compilation database*: the file that records
  the exact command line each source file is compiled with. Both cppgo commands
  below read it, so the header is parsed and the generated C++ is compiled with
  the same flags the library was built with.
- `add_library(metrics SHARED src/metrics.cpp)` builds a *shared library*: a file
  the operating system loads into a process at run time, when the program asks
  for it, instead of code copied into the Go binary at build time.
- `target_include_directories(…)` adds the project's `include/` directory to the
  search path for `#include`, which is what the compilation database then records
  as `-I<dir>`.
- `VERSION` and `SOVERSION` make CMake record a *soname* inside the library: the
  short name (`libmetrics.so.1`) the dynamic loader knows the library by, and
  resolves a program's request against. The generated package loads the shim by
  name, and the shim records your library by this soname, which is how the two
  find each other at run time.

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

That is one command, and it is the whole of phase one: the example's `make gen`
target builds the cppgo binary and the library, then runs the line it prints.
`-header`, `-package`, `-library`, `-compile-commands` and `-out` are the ones
[Generate a binding for your own library]({{< ref "how-to/generate.md" >}}) lays
out; `-status metrics::validate_name` is extra to this library, because
`validate_name` returns a 32-bit integer that means success or failure rather
than a value, and the flag is how you tell the generator that.

The report's last two declaration lines are the templates:

```
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
```

`tmpl` marks the declaration as a template, and the `-` sits where the other
lines carry a *symbol* — the name a compiled function has inside the shared
object, which C++ mangles from the namespace, class and parameter types. There is
no symbol yet because there is no concrete function yet. The count is the number
of bindings the generic API declares for the template.

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

Read the first declaration as the shape of all of them: `Accumulator[T any]` is a
generic Go type — the square brackets declare a type parameter `T` that the caller
supplies, and `any` says every type is allowed — and its one field, `obj`, is a
`*cppgo.Object`: the Go-owned block of memory the C++ object is constructed into,
kept at a fixed address so C++ can find it again ([Runtime]({{< ref "reference/runtime.md" >}}#objects)
documents it). The methods hang off that handle, so `NewAccumulator[int32](10)` is
the constructor, `i.Add(5)` the C++ `add`, and `i.Close()` the C++ destructor.
`Add` and `Total` return an `error` because the C++ members can throw; `Close`
returns `cppgo.ErrClosed` when the receiver has no live object.

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

The square brackets after each name are the type argument: `NewAccumulator[int32]`
is one instantiation of the generic constructor and `NewAccumulator[float64]` is
another, and `Twice[int64]` is the free function template at `T = int64`. The
program names three concrete types: `int32`, `float64` and `int64`. Those three
names are the entire input of the second phase. Everything else the sections above
produced is generic and stays that way.

## Step 5 — Phase two: collect the instantiations the program uses

```console
$ make scan
build/cppgo build -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name -scan ./cmd/... -scan-dir .
```

`make scan` runs the same command as `make gen`, with `build` in place of `gen`
and two flags added: `-scan` names the Go package whose instantiations to
collect, and `-scan-dir` says which directory the pattern is resolved from. The
pattern `./cmd/...` means the package in `cmd` and every package below it — the
`...` is Go's package wildcard, the same one `go test ./...` uses.

The new lines of the report are the collected types, each next to the C++
specialization it stands for:

```
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
metrics::Accumulator<double>                 tmpl   float64                      template class ::metrics::Accumulator<double>;
metrics::Accumulator<std::int32_t>           tmpl   int32                        template class ::metrics::Accumulator<std::int32_t>;
metrics::twice<std::int64_t>                 tmpl   int64                        template class ::metrics::twice<std::int64_t>;
```

The last three lines are the ones phase two added: the C++ specialization, the
Go type argument that produced it, and — in the last column — the C++ line the
command wrote. `-scan ./cmd/...` loads the Go package that matches the pattern,
finds every use of the generic API, and maps each Go type argument to a C++
type: `int32` is `std::int32_t`, `float64` is `double`, `int64` is
`std::int64_t`. `-scan` is repeatable, so a program split over several packages
passes one pattern per package.

With those types known, the command writes the explicit instantiations into the
shim and compiles it. The lines it added are visible in
[`gen/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/cppgo_shim/zz_cppgo_shim.cpp):

```cpp
// The specializations the Go program instantiates. cppgo build collected
// them from the program; adding one here is what makes its members exist.
template class ::metrics::Accumulator<double>;
template class ::metrics::Accumulator<std::int32_t>;
```

`template class ::metrics::Accumulator<double>;` is an *explicit instantiation*:
the header holds only the recipe, and this line tells the C++ compiler to write
out every member of `Accumulator<double>` — constructor, destructor, `add`,
`total`, `identity` — in this translation unit, so that the symbols exist for the
thunks to call. The leading `::` starts the name at the global namespace rather
than at whatever namespace the shim happens to be in. `class` and `struct` mean
the same thing in this position; the compiler's error messages spell it `struct`.

The free function template needs no such declaration: one thunk per collected
type is written for it — a *thunk* being a small C++ function with C linkage that
receives the arguments in a form Go and C++ both agree on and makes the real
call — and `twice<std::int64_t>` gets the one the program uses.

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
constructor through the thunk phase two wrote for `Accumulator<std::int32_t>`;
`NewAccumulator[float64]` used the other one; `Twice[int64]` used the free
function's. Without phase two the program would still have compiled, and then
failed at run time: the C++ code behind those three lines did not exist.

In your own project the consequence is a habit: the day you write
`NewAccumulator[float32]`, the Go API already accepts it, and the C++ side does
not. Re-run the scan before you build the program.

## Step 7 — When the C++ side is missing an instantiation

A type that the program uses and the shim has no specialization for is not a
crash and not a silent zero. The call returns an error that names the C++ line
to add. The error value is:

```
cppgo: metrics::Accumulator<float> has no C++ instantiation in the shim: add "template struct ::metrics::Accumulator<float>;" to the header and run `cppgo build -scan <package>`: cppgo: symbol "cppgo_tmpl_a39b2a940cf6": rtl: dlsym "cppgo_tmpl_a39b2a940cf6" in ".../gen/libcppgo_metrics.so": ...: undefined symbol: cppgo_tmpl_a39b2a940cf6
```

The first clause says what is missing, the quoted text is the exact line to add,
and the tail is the dynamic loader (`dlsym`, the C function that looks a symbol up
in a loaded library) reporting that the thunk cppgo would have generated is not
there. A program that prints the error prefixes it with its own name — the
example's `app:` — before this text.

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
| Type arguments are scalars and pointers to scalars | `bool`, the sized integers, `float32`, `float64`, and pointers to them. A `string` argument is refused with "the Go type string is not a scalar the two-phase flow carries yet": pass a `[]byte` or a scalar instead |
| Member function templates are refused | "the member function template X is not emitted by this build yet" — expose the concrete instantiation as a non-template member |
| Operators and conversion operators are refused | a template whose API is `operator+` cannot be bound yet; give it a named member |
| A public field in a class template is refused | "the public field X of a template is not emitted by this build yet" — make the field private and expose an accessor, or bind one concrete instantiation with `-instantiate` |
| A template that derives from a base | the template is emitted, but only its own members: the inherited ones are not in the Go API. Bind the base separately, or expose them through the derived class |

Each of those is a construct this build does not bind, not a configuration you
have missed. The [limits page]({{< ref "explanation/limits.md" >}}) lists each
one, and the Eigen binding, which the two-phase flow exists for, is designed and
not available yet.

## What you just did

You generated the Go API for two C++ templates without a concrete type in sight,
let the program declare the three types it uses, and had the second command turn
those three names into C++ specializations. No Go file was regenerated when the
C++ side grew, and no binding was written by hand.

## Related

- Every generated shape, including a template: [Generated API]({{< ref "reference/generated-api.md" >}}#templates).
- The command and its flags: [Command line]({{< ref "reference/cli.md" >}}).
- The same project end to end: [Build a complete Go + C++ project]({{< ref "tutorials/full-project.md" >}}).
