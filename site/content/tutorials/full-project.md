---
title: "Build a complete Go + C++ project"
description: "A CMake-built C++ library, the Go package cppgo generates from its header, and a program that calls every construct."
weight: 20
---

This lesson assembles the whole path in one project: an ordinary C++ library
built by CMake, the Go package `cppgo` generates from its header — the file
that declares what the library offers — a Go program that calls it, and the
tests that prove the calls work. The library is deliberately small — one
declaration of each kind the generator supports — so the whole mapping from C++
to Go is visible in one place.

A *binding* is the Go code that calls into a library written in another
language: Go functions that forward each call to the compiled C++. You do not
write one by hand here either; this lesson is about reading what the generator
wrote and knowing why every line of it has the shape it has.

## What you will build

By the end you will have built the library, generated the binding, run a program
that prints one line per construct, and run the suite twice.

## The scenario

The project is in the repository, at
[`examples/full`](https://github.com/Manu343726/cppgo/tree/main/examples/full),
and every command below that builds or tests the project is a target of its
[`Makefile`](https://github.com/Manu343726/cppgo/blob/main/examples/full/Makefile),
so you can run the lesson while you read it. The library it binds is called
`metrics`, and the program in
[`cmd/app`](https://github.com/Manu343726/cppgo/tree/main/examples/full/cmd/app)
calls every declaration in it.

## What you need

| Tool | Why | Version |
|---|---|---|
| Go | builds the program and runs the tests | 1.25 or newer |
| A C++ compiler | CMake picks one and builds the library with it | C++17 |
| CMake | the library's own build, and the record of how it compiles each source | 3.16 or newer |
| libclang | `cppgo gen` reads the header with it | 20 or newer |
| `make` | drives the project's flow | any |

libclang is the library form of the Clang compiler's front end: it parses C++
and hands back the declarations it found. It is needed while generating only.
The program you ship does not link it and does not need it installed.

## Step 1 — The C++ library

`metrics` is one header and one translation unit. A *header* declares what the
library offers — its types and the signatures of its functions. A *translation
unit* is one source file after the preprocessor — the pass that expands
`#include` and macro definitions before the compiler sees the file — has pulled
in everything it includes, which the compiler then turns into its own object
file. The header is the input of the whole lesson:
[`examples/full/include/metrics/metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp).
These are its declarations — a *declaration* says that a function exists and
gives its signature, and the *definition*, the body, is in the source file that
Step 2 compiles:

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

The block is worth reading once for its syntax, because it is not Go's:

- `namespace metrics { … }` groups the names; they are written
  `metrics::Histogram`, the way Go writes `metrics.Histogram` for another
  package.
- `struct Point` and `class Histogram` both declare a record. A `class` starts
  with its members private and a `struct` with them public, and `public:` and
  `private:` label the section that follows.
- `std::string`, `std::vector` and `std::int32_t` are the C++ standard library's
  string, growable array and 32-bit integer. A `std::string` and a `std::vector`
  own a heap allocation, unlike a Go `string` or slice, which is why they cannot
  simply be copied across the boundary.
- `Histogram(std::string name, std::size_t buckets)` is a *constructor*, the
  member that builds an object. `Histogram(const Histogram &other)` is the *copy
  constructor*, which builds a new object from an existing one; `const
  Histogram &` is a *reference*, a parameter that names the caller's object
  instead of copying it. `~Histogram()` is the *destructor*, the member that
  releases what the object owns.
- `std::size_t buckets() const noexcept` carries two promises: the `const`
  after the parameter list means the method does not modify the object, and
  `noexcept` means it does not throw. Neither is visible to Go as a word, and
  both decide the Go signature.
- `void observe(double value)` — `void` is the return type of a function that
  returns nothing.
- `static std::int32_t version()` — a `static` member belongs to the class
  rather than to one object, so calling it needs no object.
- `std::string name_;`, the two `std::vector` members, `std::size_t total_;` and
  `double sum_;` under `private:` are the object's state, and every one of them
  is a standard-library type or a plain number. `private:` means no code outside
  the class may read or write them, so the Go side never sees the members: it
  asks the class for copies through `name()`, `bounds()`, `counts()`, `total()`
  and `mean()`, which `cppgo gen` binds as Go methods. The three that return a
  string or a vector copy the whole container out, so their Go signatures carry
  an error beside the value; `total()` returns a `double` and cannot fail. The
  trailing underscore in the member names is a convention, not syntax: it marks
  a member of the class, so a local variable named `name` is visibly a different
  thing.
- `template <typename T>` makes `Accumulator` and `twice` *templates*: code
  written with a placeholder type that the compiler fills in at each use.
  `explicit` on a constructor forbids the implicit conversion that would
  otherwise let a bare `T` become an `Accumulator<T>`, and `: total_(start)` is
  a *member initialiser list*, which initialises fields before the constructor
  body runs. A member with its body in the class, like `T total() const`, is
  defined there; one without a body, like `name()`, is defined in the source
  file.

The declarations outside the class are *free functions*: functions in the
namespace that belong to no class, the counterpart of package-level functions
in Go.

Nothing in it knows that `cppgo` exists: no marker, no declaration that exists
only to be bound, and no `extern "C"` block — the marker a C++ function needs
before it can be found by a plain C name. Each declaration is here to show one
thing you will call from Go:

- `Histogram` is an **opaque class**: its state — a `std::string`, two
  `std::vector`s, a sample count and a running sum — is private and its size
  stays on the C++ side, so Go holds a pointer to an object C++ constructed and
  reads copies of that state through the class's own methods.
- `Point` is a **POD** — plain old data: a record with no constructors, no
  virtual functions (the C++ mechanism for overriding a method in a derived
  class) and a fixed layout, so Go can mirror its bytes. It is **passed by
  value**, in and out.
- `name`, `bounds`, `counts`, `describe` and `scaled` move **`std::string` and
  `std::vector`** across the boundary.
- `ratio` **throws** on a zero denominator. Throwing is how C++ reports a
  failure: an exception unwinds the call stack to whatever catches it, there is
  no Go equivalent, and the generated call returns an error instead.
- `validate_name` reports failure as a **status code** instead: it returns an
  integer, and a convention you state with `-status` says which values are
  failures.
- `apply` takes a **C++ function pointer** — a value that names a function and
  can be called later — which lets Go hand over a Go function.
- `Accumulator` and `twice` are **templates**, so they need a second phase
  before the program can call them.

The definitions are in
[`examples/full/src/metrics.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/src/metrics.cpp).
You do not change either file for this lesson: the binding is generated from what
is already there.

## Step 2 — Build it with CMake, and keep the compilation database

The library is built by its own build system. These are the directives of
[`examples/full/CMakeLists.txt`](https://github.com/Manu343726/cppgo/blob/main/examples/full/CMakeLists.txt):

```cmake
cmake_minimum_required(VERSION 3.16)

project(metrics LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

add_library(metrics SHARED src/metrics.cpp)

target_include_directories(metrics PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)

target_compile_options(metrics PRIVATE -Wall -Wextra -Wpedantic)

set_target_properties(metrics PROPERTIES VERSION 1.0.0 SOVERSION 1)
```

`cppgo` is not part of that file, and no generated file is compiled into
`libmetrics.so`. The directives are the ordinary ones: `project(... LANGUAGES
CXX)` names the project and says it is C++, the three `CMAKE_CXX_*` lines select
C++17 with no compiler extensions, and `add_library(metrics SHARED
src/metrics.cpp)` builds that one source file into a *shared library*: a
compiled library kept in its own file that a program loads at run time, `.so` on
Linux. `target_include_directories` puts `include/` on the include path of
anything that uses the library, `target_compile_options` adds warning flags, and
`set_target_properties` gives the library a version and a soname.

Two lines matter for what comes next:

- `CMAKE_EXPORT_COMPILE_COMMANDS ON` makes CMake write
  `build/compile_commands.json`, a *compilation database*: for every source
  file, the exact compiler command that builds it. `cppgo` reads your header
  with those exact flags, and compiles the C++ side it generates with them too,
  so the header and the binding agree on the language standard and the standard
  library. There is no second flags file to keep in step.
- `VERSION` and `SOVERSION` give the library a soname, `libmetrics.so.1` — the
  name recorded inside a shared object that anything linking against it records
  as its dependency, and the name the dynamic loader — the part of the system
  that opens a shared object when a program starts or asks for one — later looks
  up. The library `cppgo` generates records that name, so at run time the loader
  finds the copy of the library that is already open rather than a path from a
  build tree.

The library is built the way it always was:

```console
$ cmake -S . -B build
$ cmake --build build
```

You will not type those two commands again; the next step wraps them.

## Step 3 — The project's Makefile is the whole flow

The example's
[`Makefile`](https://github.com/Manu343726/cppgo/blob/main/examples/full/Makefile)
runs the flow in phases, each named after what it produces:

| Command | What it produces |
|---|---|
| `make lib` | `build/libmetrics.so` and `build/compile_commands.json`, by CMake |
| `make gen` | the generic Go API, one assembly trampoline per binding — except one that passes a Go function to C++ and a template binding: those enter C++ through the runtime — plus `cppgoFree`, the C++ thunk sources, and `gen/libcppgo_metrics.so` |
| `make scan` | the template types the program uses, added to the C++ side, which is then rebuilt |
| `make app` | `build/app`, the Go program |
| `make test` | the suite, twice |
| `make run` | builds the program and starts it |
| `make check` | proves the committed generated sources are current |

A few of those names need defining once. An *assembly trampoline* is a tiny
function whose only job is to move arguments into the registers the C ABI
expects and jump to an address; Go passes its arguments its own way, so one
trampoline is generated per bound call — whether it reaches a symbol in your
library or a thunk in the shim — except two: a binding that passes a Go function
to C++ and a template binding. Both enter C++ through the runtime and have none.
One more trampoline, `cppgoFree`, releases a `std::string` or `std::vector` the
shim copied out. The *C ABI* is the platform's convention for a C call: which
registers and which memory carry the arguments and the result. A *thunk* is a
generated C++ function that carries a call the raw symbol cannot — a
constructor, a `std::string`, a function that may throw — and the shim is the
shared object `cppgo` compiles from those thunks and links against your library.
`extern "C"` gives a thunk a plain C name rather than a C++ one, so the loader
can find it.

`make` runs `app`, then `test`, then `vet`.

Everything compiled goes under `build/`, which is ignored. The generated
*sources* under `gen/` are committed in the example, because an example is
teaching material: you can read what the generator wrote without running it, and
`make check` fails when they drift:

```console
$ make check
build/cppgo check -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name -scan ./cmd/... -scan-dir .
./gen: generated files are up to date
```

In your own project you choose whether to commit them; [Gate a generated package
in CI]({{< ref "how-to/ci.md" >}}) puts that check in a pipeline.

## Step 4 — Generate the Go package

```console
$ make gen
build/cppgo gen -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name
metrics::Histogram::Histogram(const Histogram &) thunk  cppgo_thunk_29ce97a9872c     not noexcept: an exception would unwind into Go
metrics::Histogram::Histogram(std::string, std::size_t) thunk  cppgo_thunk_fcf9a0bcc390     standard-library type: the thunk constructs, copies and destroys it
metrics::Histogram::bounds() const           thunk  cppgo_thunk_f67910ceec3d     standard-library type: the thunk constructs, copies and destroys it
metrics::Histogram::buckets() const noexcept direct _ZNK7metrics9Histogram7bucketsEv noexcept, not virtual, register class; the library exports the symbol
metrics::Histogram::counts() const           thunk  cppgo_thunk_218578361538     standard-library type: the thunk constructs, copies and destroys it
metrics::Histogram::mean() const             thunk  cppgo_thunk_b5a1c9c10995     not noexcept: an exception would unwind into Go
metrics::Histogram::name() const             thunk  cppgo_thunk_fbfdc5935e87     standard-library type: the thunk constructs, copies and destroys it
metrics::Histogram::observe(double)          thunk  cppgo_thunk_dc1615236754     not noexcept: an exception would unwind into Go
metrics::Histogram::total() const noexcept   direct _ZNK7metrics9Histogram5totalEv noexcept, not virtual, register class; the library exports the symbol
metrics::Histogram::version() noexcept       direct _ZN7metrics9Histogram7versionEv noexcept, not virtual, register class; the library exports the symbol
metrics::Histogram::~Histogram()             thunk  cppgo_thunk_426ec686bd07     not noexcept: an exception would unwind into Go
metrics::apply(double (*)(double), double)   reentrant cppgo_thunk_0cc3b25f1d56     callback: the call enters C through runtime.cgocall, so C++ may call back into Go
metrics::centroid(Point, Point) noexcept     thunk  cppgo_thunk_01e2a45f230d     a record passed or returned by value: the thunk copies it
metrics::describe(const std::string &, const Point &) thunk  cppgo_thunk_0387e43b61d4     standard-library type: the thunk constructs, copies and destroys it
metrics::ratio(double, double)               thunk  cppgo_thunk_d4489c841d72     not noexcept: an exception would unwind into Go
metrics::scaled(const std::vector<double> &, double) thunk  cppgo_thunk_56cefce36896     standard-library type: the thunk constructs, copies and destroys it
metrics::validate_name(const std::string &)  thunk  cppgo_thunk_492cf416925a     standard-library type: the thunk constructs, copies and destroys it
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
cppgo: built gen/libcppgo_metrics.so with /usr/bin/g++
```

The report has one line per declaration. Read it as four columns:

- **The declaration**, exactly as the header states it.
- **The carrier** — the mechanism that carries this call across the boundary —
  under a name you will meet again in the generated Go. `direct` means the call
  reaches the symbol the library already exports, through one of the assembly
  trampolines from Step 3; `thunk` means the C++ wrapper from Step 3 carries the
  call; `reentrant` means C++ may call back into Go while the call is running,
  which the runtime only allows on a thread it already knows is inside a foreign
  call — `runtime.cgocall` is that entry, the one cgo-generated code uses too;
  `tmpl` means a template, which needs the second phase in Step 5.
- **The symbol**, either the thunk's name or the library's *mangled name*. C++
  encodes the namespace, the class and the parameter types into one linker name,
  so `metrics::Histogram::buckets()` is `_ZNK7metrics9Histogram7bucketsEv`; that
  string is what the loader is asked for.
- **Why that carrier**, in the generator's words.

[Carriers and the construct table]({{< ref "explanation/carriers.md" >}}) is where
the three carriers are defined and compared. You do not need it to use the
package.

The one flag the header cannot replace is the last one on the command line:
`-status metrics::validate_name`. A 32-bit integer return tells the generator
nothing, so `-status` says that a non-zero value from that function is a failure
rather than a value. Everything else follows from the declarations.

Check the last line against the compiler that built your library: that is how you
know the two sides were built with the same toolchain.

## Step 5 — Phase two: the program's template instantiations

The generated package is these files and directories, in
[`examples/full/gen`](https://github.com/Manu343726/cppgo/tree/main/examples/full/gen):

| File | Contents |
|---|---|
| [`zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go) | the Go API |
| [`zz_cppgo_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo_amd64.s) | one trampoline per binding — the callback-carrying `apply` and the template bindings have none — plus `cppgoFree`, which releases a copied `std::string` or `std::vector` |
| [`cppgo_shim/`](https://github.com/Manu343726/cppgo/tree/main/examples/full/gen/cppgo_shim) | the `extern "C"` thunks, compiled into `libcppgo_metrics.so` |
| [`doc.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/doc.go) | the package comment, which you write |

A template has no Go equivalent until a type argument is chosen. Choosing it is
*instantiating* the template, and the code the compiler emits for that choice is
the *specialization*. `cppgo gen` writes the generic API, which compiles with no
concrete argument at all; which specializations the program uses is a question
only the program can answer. That is the second phase:

```console
$ make scan
build/cppgo build -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name -scan ./cmd/... -scan-dir .
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
metrics::Accumulator<double>                 tmpl   float64                      template class ::metrics::Accumulator<double>;
metrics::Accumulator<std::int32_t>           tmpl   int32                        template class ::metrics::Accumulator<std::int32_t>;
metrics::twice<std::int64_t>                 tmpl   int64                        template class ::metrics::twice<std::int64_t>;
```

`cppgo build` reads the program's Go packages, type-checks them, and records
every `Accumulator[...]` and `Twice[...]` it finds. For each one it writes an
*explicit instantiation* into the shim — the C++ line `template class
::metrics::Accumulator<double>;`, which tells the compiler to emit that
specialization even though nothing in the shim calls it directly — plus one
thunk per member, and then recompiles the shim. The consequence in your own
project: the day you write
`Accumulator[float32]`, the Go package needs no regeneration — the specialization
goes into the C++ side, so re-run the scan.

The two phases, and what limits the generic API today, are
[Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## Step 6 — What the generated package looks like

Two entry points are not generated from a C++ declaration, because they load the
C++ side rather than call it. From
[`zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
//
// path names the shim cppgo compiled beside this package. A path of "" uses the
// name the package was generated with. It is safe to call Open more than once;
// the first successful call wins.
func Open(path string) error { return cppgoOpen(path) }

// CppgoLibraryPath overrides where the user's library is loaded from, for a
// deployment that does not keep it beside the shim. It must be set before the
// first Open: the two objects are loaded once, and a later change is not
// observed.
//
// The shim names the library by its soname, so a library that declares one is
// found wherever this points. A library built without a soname is loaded from
// the path here, and the shim still needs the loader to find it by name: keep
// it beside the shim, or put its directory on LD_LIBRARY_PATH.
var CppgoLibraryPath string
```

Two shared objects load as a pair: the shim, and the library it was linked
against. The shim names the library by its soname, so the package looks for it
beside the shim by default. This example keeps the two in different directories,
so its program sets `CppgoLibraryPath` before the first `Open`, and
`CPPGO_LIBRARY_PATH` in the environment overrides it. `LD_LIBRARY_PATH`, named in
the comment above, is the environment variable the dynamic loader adds to its
search path. A deployment that copies `libmetrics.so.1` beside the shim needs
neither.

A POD's Go struct is proven against the C++ layout at compile time:

```go
// Point is metrics::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.
type Point struct {
	X float64
	Y float64
}

// The C++ layout is proven at compile time: a wrong size, alignment or
// field offset does not compile.
var _ [16]byte = [unsafe.Sizeof(Point{})]byte{}
var _ [8]byte = [unsafe.Alignof(Point{})]byte{}
var _ [0]byte = [unsafe.Offsetof(Point{}.X)]byte{}
var _ [8]byte = [unsafe.Offsetof(Point{}.Y)]byte{}
```

Each `var _ [N]byte` line assigns one array to another: the length on the left is
the number the C++ side computes, and the length on the right is what Go
measures with `unsafe.Sizeof`, `unsafe.Alignof` or `unsafe.Offsetof`. Go requires
the two array types to be identical, so if the C++ layout ever changed, that line
would stop compiling — a check that costs nothing at run time and cannot be
forgotten.

And every binding carries the declaration it came from, plus the carrier that was
chosen, in its doc comment. From the same file:

```go
// Buckets calls metrics::Histogram::buckets.
//
// C++: metrics::Histogram::buckets() const noexcept, _ZNK7metrics9Histogram7bucketsEv (direct, noexcept, not virtual, register class; the library exports the symbol), verified against libmetrics.so
func (h *Histogram) Buckets() uint64 {
```

That comment is why you never have to guess: a Go method traces back to the line
in the header that produced it.

## Step 7 — Create and destroy the C++ object

Start with the class. From the header:

```cpp
class Histogram {
public:
    Histogram(std::string name, std::size_t buckets);

    Histogram(const Histogram &other);

    ~Histogram();
```

The generated Go keeps the object where it belongs — in C++:

```go
// Histogram is metrics::Histogram. The object belongs to C++; obj is where it lives.
type Histogram struct {
	obj *cppgo.Object
}

// NewHistogram constructs a metrics::Histogram.
//
// C++: metrics::Histogram::Histogram(std::string, std::size_t), _ZN7metrics9HistogramC1ENSt7__cxx1112basic_stringIcSt11char_traitsIcESaIcEEEm (thunk cppgo_thunk_fcf9a0bcc390, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func NewHistogram(name string, buckets uint64) (*Histogram, error)

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has no live object.
//
// C++: metrics::Histogram::~Histogram(), _ZN7metrics9HistogramD1Ev (thunk cppgo_thunk_426ec686bd07, not noexcept: an exception would unwind into Go), verified against libmetrics.so
func (h *Histogram) Close() error
```

`cppgo.Object` is the handle that owns the object on the Go side: a block of Go
memory whose address C++ uses, released by `Close`.
[The Go runtime and a native call]({{< ref "explanation/go-runtime.md" >}})
explains why that block must be pinned in place, and why the C++ object cannot
live in a Go local variable.

The program creates one and hands its lifetime to `defer`, from
[`cmd/app/main.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/main.go):

```go
h, err := metrics.NewHistogram("latency", 4)
if err != nil {
	return err
}
defer h.Close()
```

Three rules are visible there:

- **The constructor returns an error.** The C++ constructor takes a `std::string`
  and can throw, so the Go constructor can fail.
- **`Close` releases the object.** There is no finalizer and no `runtime.KeepAlive`
  for you to write: call `Close`, and `defer` is the usual place.
- **A second `Close` is an error, not a crash.** It returns `cppgo.ErrClosed`,
  which the suite asserts.

## Step 8 — Call the methods: direct and thunk

The class exposes five members in two kinds. From the header:

```cpp
    std::size_t buckets() const noexcept;
    double total() const noexcept;

    static std::int32_t version() noexcept;

    void observe(double value);
    double mean() const;
```

The two `noexcept` accessors and the static member become calls that cannot fail.
From the generated Go — the doc comments are the report's lines again:

```go
// Buckets calls metrics::Histogram::buckets.
//
// C++: metrics::Histogram::buckets() const noexcept, _ZNK7metrics9Histogram7bucketsEv (direct, noexcept, not virtual, register class; the library exports the symbol), verified against libmetrics.so
func (h *Histogram) Buckets() uint64

// Total calls metrics::Histogram::total.
//
// C++: metrics::Histogram::total() const noexcept, _ZNK7metrics9Histogram5totalEv (direct, noexcept, not virtual, register class; the library exports the symbol), verified against libmetrics.so
func (h *Histogram) Total() float64

// HistogramVersion calls metrics::Histogram::version.
//
// C++: metrics::Histogram::version() noexcept, _ZN7metrics9Histogram7versionEv (direct, noexcept, not virtual, register class; the library exports the symbol), verified against libmetrics.so
func HistogramVersion() int32
```

The members that can throw become calls that return an error:

```go
// Observe calls metrics::Histogram::observe.
//
// C++: metrics::Histogram::observe(double), _ZN7metrics9Histogram7observeEd (thunk cppgo_thunk_dc1615236754, not noexcept: an exception would unwind into Go), verified against libmetrics.so
func (h *Histogram) Observe(value float64) error

// Mean calls metrics::Histogram::mean.
//
// C++: metrics::Histogram::mean() const, _ZNK7metrics9Histogram4meanEv (thunk cppgo_thunk_b5a1c9c10995, not noexcept: an exception would unwind into Go), verified against libmetrics.so
func (h *Histogram) Mean() (float64, error)
```

Notice what the second `C++:` line says: `version` is a static member, and it
became a **package-level function**, `HistogramVersion`, not a method — there is
no object to hang it on.

The program calls them the way it calls any Go package:

```go
for _, sample := range []float64{0.1, 0.6, 0.9} {
	if err := h.Observe(sample); err != nil {
		return err
	}
}
```

```go
mean, err := h.Mean()
if err != nil {
	return err
}
fmt.Printf("histogram %q: %d buckets, %v samples\n", name, h.Buckets(), h.Total())
fmt.Printf("  bounds: %v\n", bounds)
fmt.Printf("  counts: %v\n", counts)
fmt.Printf("  mean:   %v\n", mean)
```

`name`, `bounds` and `counts` there are the values the three value-returning
methods returned; their calls are in Step 10.

The rule the Go signatures state is the rule the C++ header states: **a call that
cannot fail has no error result, and a call that can fail has one.** `Buckets` and
`Total` cannot be checked because there is nothing to check; `Observe` and `Mean`
compile only if you handle their error.

The difference between the `direct` line and the `thunk` line is what the call
costs, not what it means. [Carriers and the construct table]({{< ref "explanation/carriers.md" >}})
has the mechanism, and [Performance]({{< ref "explanation/benchmarks.md" >}}) has
the measurement.

## Step 9 — A copy is the C++ copy constructor

The class declares a copy constructor:

```cpp
    Histogram(const Histogram &other);
```

The generated Go exposes it as `Clone`:

```go
// Clone copies the object through its copy constructor.
//
// C++: metrics::Histogram::Histogram(const Histogram &), _ZN7metrics9HistogramC1ERKS0_ (thunk cppgo_thunk_29ce97a9872c, not noexcept: an exception would unwind into Go), verified against libmetrics.so
func (h *Histogram) Clone() (*Histogram, error)
```

And the program uses it for what a copy constructor is for — an independent
object:

```go
clone, err := h.Clone()
if err != nil {
	return err
}
if err := clone.Observe(0.3); err != nil {
	clone.Close()
	return err
}
cloneTotal := clone.Total()
if err := clone.Close(); err != nil {
	return err
}
fmt.Printf("  clone:  %v samples after one more, original %v\n", cloneTotal, h.Total())
```

`Clone` is not a Go copy of a pointer. It hands the object to the C++ copy
constructor, so the clone owns its own state: the sample the clone records does
not appear in the original, which the program prints and the suite asserts.

The clone is a second object with a second lifetime, so it gets its own `Close`.

## Step 10 — `std::string` and `std::vector` arrive as Go values

From the header:

```cpp
    std::string name() const;
    std::vector<double> bounds() const;
    std::vector<std::size_t> counts() const;

std::string describe(const std::string &prefix, const Point &p);

std::vector<double> scaled(const std::vector<double> &values, double factor);
```

Five declarations, and three Go types between them: `string`, `[]float64` and
`[]uint64`. All five return a value; `describe` also takes a string and a
`Point`, which is why it is shown in the next step.

```go
// Name calls metrics::Histogram::name.
//
// C++: metrics::Histogram::name() const, _ZNK7metrics9Histogram4nameB5cxx11Ev (thunk cppgo_thunk_fbfdc5935e87, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func (h *Histogram) Name() (string, error)

// Bounds calls metrics::Histogram::bounds.
//
// C++: metrics::Histogram::bounds() const, _ZNK7metrics9Histogram6boundsEv (thunk cppgo_thunk_f67910ceec3d, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func (h *Histogram) Bounds() ([]float64, error)

// Counts calls metrics::Histogram::counts.
//
// C++: metrics::Histogram::counts() const, _ZNK7metrics9Histogram6countsEv (thunk cppgo_thunk_218578361538, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func (h *Histogram) Counts() ([]uint64, error)

// Scaled calls metrics::scaled.
//
// C++: metrics::scaled(const std::vector<double> &, double), _ZN7metrics6scaledERKSt6vectorIdSaIdEEd (thunk cppgo_thunk_56cefce36896, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func Scaled(values []float64, factor float64) ([]float64, error)
```

The program reads a string and two vectors out of the object, and passes one in:

```go
name, err := h.Name()
if err != nil {
	return err
}
bounds, err := h.Bounds()
if err != nil {
	return err
}
counts, err := h.Counts()
if err != nil {
	return err
}
```

```go
scaled, err := metrics.Scaled([]float64{1, 2, 3}, 2)
if err != nil {
	return err
}
fmt.Printf("scaled([1 2 3], 2) = %v\n", scaled)
```

A value crosses as a copy, in both directions: the returned `string` and slices
are Go's, and writing to the returned slice does not touch the C++ vector. Passing
a slice in copies it into a `std::vector`; a `nil` slice is an empty vector, which
the suite asserts.

`std::vector<std::size_t>` becomes `[]uint64` rather than `[]int`, so what the Go
side sees has the width the C++ side computed.

## Step 11 — A struct passes by value

From the header:

```cpp
struct Point {
    double x;
    double y;
};

Point centroid(Point a, Point b) noexcept;
```

`Point` is a plain Go struct whose layout was checked at compile time (Step 6),
and `centroid` returns it by value:

```go
// Centroid calls metrics::centroid.
//
// C++: metrics::centroid(Point, Point) noexcept, _ZN7metrics8centroidENS_5PointES0_ (thunk cppgo_thunk_01e2a45f230d, a record passed or returned by value: the thunk copies it), verified against libmetrics.so
func Centroid(a Point, b Point) Point
```

The program builds the two corners and takes the midpoint:

```go
origin, err := metrics.NewPoint()
if err != nil {
	return err
}
corner := metrics.Point{X: 2, Y: 4}
mid := metrics.Centroid(*origin, corner)
fmt.Printf("centroid((%v, %v), (%v, %v)) = (%v, %v)\n",
	origin.X, origin.Y, corner.X, corner.Y, mid.X, mid.Y)
```

No lifetime is involved: `Point` is 16 bytes with no pointer into C++ memory, so
the call copies it in and copies the result out. `NewPoint` returns a zero value.

Where C++ takes the struct by reference, Go passes a pointer instead — the same
declaration that takes `const Point &` takes a `*Point`:

```go
// Describe calls metrics::describe.
//
// C++: metrics::describe(const std::string &, const Point &), _ZN7metrics8describeERKNSt7__cxx1112basic_stringIcSt11char_traitsIcESaIcEEERKNS_5PointE (thunk cppgo_thunk_0387e43b61d4, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func Describe(prefix string, p *Point) (string, error)
```

```go
described, err := metrics.Describe("corner", &corner)
if err != nil {
	return err
}
fmt.Printf("describe = %q\n", described)
```

## Step 12 — Failures arrive as errors: a throw and a status code

Two declarations fail in two different ways. From the header:

```cpp
double ratio(double numerator, double denominator);

std::int32_t validate_name(const std::string &name);
```

`ratio` throws, so its Go signature carries the error:

```go
// Ratio calls metrics::ratio.
//
// C++: metrics::ratio(double, double), _ZN7metrics5ratioEdd (thunk cppgo_thunk_d4489c841d72, not noexcept: an exception would unwind into Go), verified against libmetrics.so
func Ratio(numerator float64, denominator float64) (float64, error)
```

`validate_name` reports failure as a status code — 0 for success, anything else
for failure — and the `-status` flag is what told the generator so. Its Go
signature returns only the error, because the integer was never a value:

```go
// ValidateName calls metrics::validate_name.
//
// C++: metrics::validate_name(const std::string &), _ZN7metrics13validate_nameERKNSt7__cxx1112basic_stringIcSt11char_traitsIcESaIcEEE (thunk cppgo_thunk_492cf416925a, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func ValidateName(name string) error
```

The program calls both, and exercises each failure path:

```go
ratio, err := metrics.Ratio(9, 4)
if err != nil {
	return err
}
fmt.Printf("ratio(9, 4) = %v\n", ratio)
if _, err := metrics.Ratio(1, 0); err != nil {
	fmt.Printf("ratio(1, 0) failed as expected: %v\n", err)
} else {
	return fmt.Errorf("ratio(1, 0) must fail")
}
```

```go
if err := metrics.ValidateName(""); err != nil {
	fmt.Printf("validate_name(\"\") failed as expected: %v\n", err)
} else {
	return fmt.Errorf("validate_name(\"\") must fail")
}
```

Read the two failures together. The caller handles one kind of error either way,
but the values are not interchangeable — only the throw carries the C++ message.
The suite inspects the error a throw produces:

```go
var call *cppgo.CallError
if !errors.As(err, &call) {
	t.Fatalf("Ratio(1, 0) = %T, want *cppgo.CallError", err)
}
if call.Symbol != "metrics::ratio" {
	t.Fatalf("the error names %q, want metrics::ratio", call.Symbol)
}
```

A `*cppgo.CallError` names the C++ declaration that failed and carries the
message the library stored, so a log line says which C++ function refused rather
than only that something went wrong. The status call has no message of its own;
its error names the declaration and says the call failed without one.

[Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}) covers the
remaining paths and what each error type means.

## Step 13 — C++ calls a Go function

From the header:

```cpp
double apply(double (*fn)(double), double value);
```

The parameter is a C++ function pointer. The Go side hands over a Go function,
and the generated signature takes it as a value:

```go
// Apply calls metrics::apply.
//
// C++: metrics::apply(double (*)(double), double), _ZN7metrics5applyEPFddEd (reentrant cppgo_thunk_0cc3b25f1d56, callback: the call enters C through runtime.cgocall, so C++ may call back into Go), verified against libmetrics.so
func Apply(fn func(float64) float64, value float64) (float64, error)
```

The program passes an anonymous function:

```go
applied, err := metrics.Apply(func(v float64) float64 { return v * 3 }, 4)
if err != nil {
	return err
}
fmt.Printf("apply(v*3, 4) = %v\n", applied)
```

The function you pass runs **while the C++ call is running**, and the result C++
computes from it comes back as the call's result. The suite asserts the direction
by counting the calls the Go function received inside the call.

This is the one call that does not go straight to the library: it enters C through
`runtime.cgocall` so that C++ may re-enter Go, which is why the report calls it
`reentrant` rather than `thunk`.
[Callbacks]({{< ref "explanation/architecture.md" >}}#callbacks) has the rule that
keeps a callback from escaping the call it was passed to.

## Step 14 — Templates: the API now, the specializations from the program

From the header:

```cpp
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
```

A Go generic mirrors each one, with the type argument carried through:

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

The program instantiates them, and every instantiation it writes is one
`make scan` had collected:

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
```

```go
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
```

```go
twice, err := metrics.Twice[int64](21)
if err != nil {
	return err
}
fmt.Printf("twice[int64](21) = %v\n", twice)
```

The bridge between Go's type argument and the C++ specialization is the step the
scan performed: `int32` became `std::int32_t`, `int64` became `std::int64_t`, and
`Accumulator` got a static member exposed as a package-level function,
`AccumulatorIdentity`, the same way `version` did.

Writing `NewAccumulator[float32]` in your own program is the moment to re-run the
scan: the Go API already accepts it, and the C++ side needs the new
specialization before the program can run.

## Step 15 — Run it

`make run` builds the program and starts it. The program prints:

```console
$ ./build/app
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

Every line is one of the steps above: the static function, the opaque class with
its strings and vectors, the clone, the struct by value, the slice, the two
failure paths, the callback and the templates. The program returns a non-zero
exit status if a call that must succeed does not.

## Step 16 — Test it

`make test` regenerates the binding and rescans the program first; these are
the two test passes it runs:

```console
$ CGO_ENABLED=0 go test -count=1 -v ./...
=== RUN   TestHistogramValues
--- PASS: TestHistogramValues (0.00s)
=== RUN   TestCloneIsIndependent
--- PASS: TestCloneIsIndependent (0.00s)
=== RUN   TestPointByValue
--- PASS: TestPointByValue (0.00s)
=== RUN   TestVectorCopiesInAndOut
--- PASS: TestVectorCopiesInAndOut (0.00s)
=== RUN   TestThrowingCall
--- PASS: TestThrowingCall (0.00s)
=== RUN   TestStatusCall
--- PASS: TestStatusCall (0.00s)
=== RUN   TestCallback
--- PASS: TestCallback (0.00s)
=== RUN   TestGenericInstantiations
--- PASS: TestGenericInstantiations (0.00s)
=== RUN   TestRun
...
--- PASS: TestRun (0.00s)
PASS
ok  	github.com/Manu343726/cppgo/examples/full/cmd/app	0.017s
?   	github.com/Manu343726/cppgo/examples/full/gen	[no test files]
$ CGO_ENABLED=1 go test -race -count=1 ./...
ok  	github.com/Manu343726/cppgo/examples/full/cmd/app	1.043s
?   	github.com/Manu343726/cppgo/examples/full/gen	[no test files]
```

One test per construct, in
[`cmd/app/app_test.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/app_test.go):

| Test | The construct it pins |
|---|---|
| `TestHistogramValues` | the constructor, the accessors, the sample loop, the string and the two vectors |
| `TestCloneIsIndependent` | `Clone` calls the copy constructor, and a second `Close` is `cppgo.ErrClosed` |
| `TestPointByValue` | a 16-byte POD in and out by value |
| `TestVectorCopiesInAndOut` | a slice into a `std::vector`, the result into a fresh slice, and `nil` as empty |
| `TestThrowingCall` | a C++ throw arrives as a `*cppgo.CallError` naming `metrics::ratio` |
| `TestStatusCall` | status 0 is success, a non-zero status is an error naming `metrics::validate_name` |
| `TestCallback` | the Go function runs inside the C++ call |
| `TestGenericInstantiations` | the two `Accumulator` specializations, its default constructor and static member, and `Twice[int64]` |

The suite runs twice, and the two passes measure different things. The first sets
`CGO_ENABLED=0`: *cgo*, Go's built-in way to call C, needs a C compiler at build
time, and the generated package, the program and these tests build and run with
it switched off. The second adds the race detector, which is built on cgo and
needs it enabled. One command cannot measure both properties.

## What you just did

You built a C++ library with its own build system, generated a Go package from
its header without changing either, and wrote a program whose calls fail the way
the C++ declarations say they fail. Two commands did the binding: `cppgo gen` for
the API, and `cppgo build -scan` for the template specializations the program
uses.

## Where to go next

- Bind your own library: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- A template-heavy library: [Bind a C++ template library]({{< ref "how-to/templates.md" >}}).
- Ship the result: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
- Look up a signature: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Understand the machinery: [Architecture]({{< ref "explanation/architecture.md" >}})
  and [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
