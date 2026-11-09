---
title: "Bind your first C++ class"
description: "From a C++ header to a passing Go test."
weight: 10
---

This lesson takes one small C++ class from a header to a Go test that calls it.
It assumes you write Go and have never called C++ before, so it defines each
term the first time it uses one.

A *binding* is the Go code that calls into a library written in another
language: Go functions that forward each call to the compiled C++. You will not
write one by hand. You will point `cppgo` at the header and at the way the
project already compiles it, and it will write the Go package for you.

## What you will build

By the end of the lesson you will have built the C++ library, generated the Go
package that calls it, and run a program whose output includes the vector's norm,
an error raised by an out-of-range index in C++, and the vector after a scale.
The lesson closes with the example's own test suite passing twice: once with
*cgo* — Go's built-in way to call C, which needs a C compiler at build time —
switched off, and once under the race detector.

## What you need

| Tool | Why | Version |
|---|---|---|
| Go | builds the generated package | 1.25 or newer (`go.mod` declares `go 1.25.0`) |
| A C++ compiler | `cppgo` builds the C++ side with it | C++17; the example uses `g++` |
| libclang | `cppgo gen` reads the header with it | **20 or newer** |
| `make` | runs the example's own build | any |

libclang is the library form of the Clang compiler's front end: it parses C++
and hands back the declarations it found. It is needed only while generating.
The program you ship does not link it and does not need it installed. The
version floor is 20: older releases lack a function the generator uses, and
generation fails at load time if yours is older.

## The scenario

The example this lesson uses is in the repository:
[`examples/mathx`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx).
It is a small C++17 library with one class, a two-dimensional vector, built by
its own
[`Makefile`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/Makefile);
nothing in it mentions cppgo. Every command that builds or tests the example
is one of that `Makefile`'s targets, so you can run them while you read.

## Step 1 — Look at the C++

Here is the header the lesson binds,
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp).
A *header* is the file that declares what a C++ library offers — its types and
the signatures of its functions — while the bodies live in a separate source
file and are compiled into the library. Nothing in this one knows that `cppgo`
exists: no annotation, no macro, nothing added in order to be bound.

```cpp
namespace mathx {

struct Point {
    double x;
    double y;
};

class Vector {
public:
    Vector(double x, double y);

    Vector(const Vector &other);

    ~Vector();

    double x() const { return x_; }

    double y() const;

    double norm() const;

    double dot(const double *ys, std::size_t n) const;

    double at(std::size_t index) const;

    void validate() const;

    void scale(double k) noexcept;

    static int dims();

private:
    double x_;
    double y_;
};

Point midpoint(Point a, Point b);

void saxpy(double a, const double *x, double *y, std::size_t n);

} // namespace mathx
```

Some of that syntax is not Go's. In the order the block uses it:

- `namespace mathx { … }` groups the names; they are written `mathx::Vector`,
  the way Go writes `mathx.Vector` for another package.
- `struct Point` and `class Vector` both declare a record of fields and
  functions. A `class` starts with its members private and a `struct` with them
  public, and the `public:` and `private:` labels mark the section that follows.
- `Vector(double x, double y)` is a *constructor*: a member named after the
  class, holding the code that builds an object. `Vector(const Vector &other)`
  is the *copy constructor*, which builds a new object from an existing one;
  `const Vector &` is a *reference*, a parameter that names the caller's object
  instead of copying it. `~Vector()` is the *destructor*, which releases what
  the object owns when it is destroyed.
- `double x() const { return x_; }` is defined where it is declared, body
  included, while the other members are only declared here and defined in the
  source file. The `const` after a parameter list is a promise that the method
  does not modify the object.
- `static int dims()` — a `static` member belongs to the class rather than to
  one object, so calling it needs no object.
- `std::size_t` is C++'s unsigned integer type for sizes and indexes, and
  `std::` is the namespace of the C++ standard library.

`midpoint` and `saxpy` are *free functions*: functions in the namespace that
belong to no class. The lesson does not call them, and the generator binds them
anyway.

Three things in this header show up in the Go API you will get, and it is worth
knowing them before you generate:

- `scale` is `noexcept`: a promise, written in the declaration, that the
  function does not throw. Go needs no error for a call that cannot fail, so
  `Scale` returns nothing.
- `norm`, `at` and `validate` can throw. C++ reports a failure by *throwing* an
  exception — an object that unwinds the call stack until something catches it
  — and there is no Go equivalent, so their methods return an error. `at`
  throwing for an index other than 0 or 1 is the failure this lesson uses at
  the end.
- `dot` takes a `const double *` and a length. A *pointer* is an address, and
  Go passes a slice to C++ as the address of its first element plus its length,
  which is why `Dot` takes a `[]float64` and the data is not copied.

The definitions are in
[`examples/mathx/src/vector.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/src/vector.cpp).

## Step 2 — Build the library the way you always would

The C++ side is an ordinary *shared library*, built by its own build system with
its own flags. A shared library is a compiled library kept in its own file that
a program loads at run time instead of copying into its executable; on Linux the
file ends in `.so`, for *shared object*.

```console
$ make lib
g++ -std=c++17 -O2 -fPIC -Wall -Wextra -Iinclude -shared -Wl,-soname,libmathx.so -o build/libmathx.so src/vector.cpp
```

Every flag there is one you would write yourself: `-std=c++17` picks the C++17
dialect, `-O2` the optimisation level, `-fPIC` makes the code
position-independent, which is what a shared object needs in order to be loaded
at an address chosen at run time, `-Wall -Wextra` turn warnings on, `-Iinclude`
adds `include/` to the *include path* — the directories the compiler searches
for a `#include` — and `-shared` asks for a shared object rather than an
executable.

Keep the `-soname` flag. A *soname* is a name recorded inside a shared object,
and the name any object linked against it records as its dependency. The
dynamic loader — the part of the system that opens a shared object when a
program starts or asks for one — finds the library by that name at run time
instead of by the path it was built at. The C++ library cppgo generates records
`libmathx.so` as the library it needs.

`cppgo` is not involved in this command, and no generated file is compiled into
this library.

## Step 3 — Tell cppgo how your header compiles

`cppgo gen` needs the include paths and the language standard your compiler
uses. The example states them in
[`examples/mathx/.clangd`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/.clangd),
a small configuration that clang tools read:

```yaml
# examples/mathx/.clangd
CompileFlags:
  Add: [-std=c++17, -Iinclude]
```

`CompileFlags:` names the section and `Add:` appends its flags to every compile,
so this says the same `-std=c++17 -Iinclude` as the `g++` line above.

If your project already produces a *compilation database* —
`compile_commands.json`, a file that lists for every source file the exact
compiler command that builds it — pass it instead with
`-compile-commands build/compile_commands.json`. CMake and Meson write one, and
cppgo reads the entry for each header from it.

Do not skip this because the header compiles: with the wrong include path the
parser sees a different type and the binding is wrong rather than failed. When
cppgo finds no configuration at all it says so and assumes one, in a warning
that begins `the compilation flags for include/mathx/vector.hpp are an
assumption`, names the flags it assumed in the parenthesis, and then parses with
them.

## Step 4 — Generate the binding

```console
$ make gen
build/cppgo gen -header include/mathx/vector.hpp -package mathx -library build/libmathx.so -out .
mathx::Vector::Vector(const Vector &)        thunk  cppgo_thunk_1af92e8a16a6     not noexcept: an exception would unwind into Go
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c482bce3e     not noexcept: an exception would unwind into Go
mathx::Vector::at(std::size_t) const         thunk  cppgo_thunk_79aeb346bd10     not noexcept: an exception would unwind into Go
...
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
...
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

The report has one line per declaration it bound, and the last line names the
compiler it used to build the C++ side.

Each line gives four things: the declaration, the *carrier* — the mechanism
that carries this call across the boundary — the symbol the call is reached
through, and the generator's reason. A carrier of `direct` means the call goes
to a symbol the library itself exports; `thunk` means a generated C++ function,
a *thunk*, makes the call for you. The symbol is either a thunk name or the
library's *mangled name*: C++ encodes the namespace, the class and the parameter
types into one linker symbol, so `mathx::Vector::scale(double)` is
`_ZN5mathx6Vector5scaleEd`, and the loader resolves exactly that string.

Why one declaration reads `direct` and the rest do not is
[how it works]({{< ref "explanation/architecture.md" >}}), not something you
need in order to use the package.

Generation also wrote the Go package beside the header's project, plus a small
C++ library that your program loads at run time. That library is the *shim*:
C++ that cppgo generates, compiles and links against your own library, so that
the calls the library's own symbols cannot carry still have a C++ function to
enter. Commit the generated sources with your project; the C++ library is a
build output. The full list of files is in
[Architecture]({{< ref "explanation/architecture.md" >}}#the-generated-files),
and the example's
[`Makefile`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/Makefile)
shows which ones it ignores.

## Step 5 — Call it from Go

`cppgo gen` turned the header in Step 1 into a Go package. The lesson calls
these declarations, all quoted from
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
Vector(double x, double y);
~Vector();

double x() const { return x_; }
double y() const;
double norm() const;

double dot(const double *ys, std::size_t n) const;
double at(std::size_t index) const;

void scale(double k) noexcept;
```

They arrive as these Go declarations, in
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go);
the bodies are generated, and the signature and the comment are the contract:

```go
// NewVector constructs a mathx::Vector.
func NewVector(x float64, y float64) (*Vector, error)

// X calls mathx::Vector::x.
func (v *Vector) X() (float64, error)

// Y calls mathx::Vector::y.
func (v *Vector) Y() (float64, error)

// Norm calls mathx::Vector::norm.
func (v *Vector) Norm() (float64, error)

// Dot calls mathx::Vector::dot.
func (v *Vector) Dot(ys []float64) (float64, error)

// At calls mathx::Vector::at.
func (v *Vector) At(index uint64) (float64, error)

// Scale calls mathx::Vector::scale.
func (v *Vector) Scale(k float64)

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has no live object.
func (v *Vector) Close() error
```

The `Close` comment names `cppgo.ErrClosed`: the error the package returns when
there is nothing to call or release — an object that is already closed, or a
library that was never opened.

The package also has two names that are not generated from a C++ declaration,
because they are about loading the two shared objects rather than calling them:
`Open` loads them, and `CppgoLibraryPath` says where your library is when it is
not beside them.

```go
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
func Open(path string) error

// CppgoLibraryPath overrides where the user's library is loaded from, for a
// deployment that does not keep it beside the shim.
var CppgoLibraryPath string
```

Two rules are visible in the generated signatures, and they hold for every
generated package:

- **A call that can fail returns an error; a call that cannot does not.** `Norm`
  returns `(float64, error)`; `Scale` returns nothing. There is no error value
  that is always nil.
- **A C++ object is a Go value with a lifetime.** A constructor returns
  `(*Vector, error)` and `Close` releases the object. There is no finalizer:
  call `Close`.

Each declaration also carries a comment naming the C++ declaration it came from,
so you can always trace a generated method back to the header.

Two shared objects load as a pair at run time: the shim, and the library the
shim was linked against. The package looks for the second one beside the first,
so keep them in one directory:

```console
$ cp build/libmathx.so .        # puts your library beside the generated one
```

Here is a program that calls the package. Put it in a `main` package inside the
example's module — a directory of your own beside the generated package, `demo`
in this example — and run it with `go run ./demo`:

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/examples/mathx"
)

func main() {
	if err := mathx.Open("./libcppgo_mathx.so"); err != nil {
		log.Fatal(err)
	}

	v, err := mathx.NewVector(3, 4)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	norm, _ := v.Norm()
	dot, _ := v.Dot([]float64{1, 0})
	fmt.Printf("norm = %v, dot = %v\n", norm, dot)

	if _, err := v.At(2); err != nil {
		fmt.Println("At(2):", err)
		var call *cppgo.CallError
		fmt.Println("  is a *cppgo.CallError:", errors.As(err, &call))
	}

	v.Scale(2)
	x, _ := v.X()
	y, _ := v.Y()
	fmt.Printf("scaled = (%v, %v)\n", x, y)
	fmt.Println("close:", v.Close())
	fmt.Println("close again:", v.Close())
}
```

Run from `examples/mathx`, it prints:

```console
norm = 5, dot = 3
At(2): cppgo: mathx::Vector::at: Vector::at: index out of range
  is a *cppgo.CallError: true
scaled = (6, 8)
close: <nil>
close again: library is closed
```

Read the third and fourth lines together. The C++ `at` failed and the Go caller
got an error naming the declaration and carrying the library's message; the
caller does not have to know which C++ mechanism reported it. `*cppgo.CallError`
is that error's type — it names the C++ declaration that failed and wraps the
message the C++ side stored — and `errors.As` is how Go checks for it.

The last two lines are the lifetime rule. The first `Close` released the object
and returned nil. The second one finds nothing to release and returns
`cppgo.ErrClosed`, whose message is `library is closed`.

## Step 6 — Run the tests

The example's test suite does the same thing for every binding, once with cgo
disabled and once under the race detector. One property of the generated package
is that it uses no cgo at all, so the first pass runs with `CGO_ENABLED=0`.
`make test` regenerates the binding first; these are the two passes it runs:

```console
$ CGO_ENABLED=0 go test -count=1 -v .
=== RUN   TestConstructorAndAccessors
--- PASS: TestConstructorAndAccessors (0.00s)
...
--- PASS: TestCloseLifetime (0.00s)
PASS
ok  	github.com/Manu343726/cppgo/examples/mathx	0.021s
$ CGO_ENABLED=1 go test -race -count=1 .
ok  	github.com/Manu343726/cppgo/examples/mathx	1.046s
```

The first pass proves the no-cgo rule: the generated package and the cppgo
runtime it imports build and run with `CGO_ENABLED=0`. The second pass needs cgo because the race
detector is built on it; the two properties cannot be measured by one command.

## What you just did

You wrote no binding and changed no C++ build. You told cppgo where the
declarations are and how they are compiled, and you got a Go package that calls
the class, with the C++ failure paths arriving as Go errors.

## Where to go next

- Bind your own library: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- Ship the C++ side: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
- Handle failures: [Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}).
- Understand the machinery: [Architecture]({{< ref "explanation/architecture.md" >}})
  and [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- Look up a signature: [Generated API]({{< ref "reference/generated-api.md" >}}).
