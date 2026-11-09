---
title: "Bind your first C++ class"
description: "From a C++ header to a passing Go test."
weight: 10
---

This lesson takes one small C++ class from a header to a Go test that calls it.
It assumes you write Go and have never called C++ before, so it defines each
term the first time it uses one.

You will not write a binding by hand. You will point `cppgo` at the header and at
the way the project already compiles it, and it will write a Go package that
calls the class.

## What you will build

By the end of the lesson you will have built the C++ library, generated the Go
package that calls it, and run a program whose output includes the vector's norm,
an error raised by an out-of-range index in C++, and the vector after a scale.
The lesson closes with the example's own test suite passing, once with cgo
disabled and once under the race detector.

## What you need

| Tool | Why | Version |
|---|---|---|
| Go | builds the generated package | 1.25 or newer (`go.mod` declares `go 1.25.0`) |
| A C++ compiler | `cppgo` builds the C++ side with it | C++17; the example uses `g++` |
| libclang | `cppgo gen` reads the header with it | **20 or newer** |
| `make` | runs the example's own build | any |

libclang is needed only while generating. The program you ship does not link it
and does not need it installed. The version floor is 20: older releases lack a
function the generator uses, and generation fails at load time if yours is older.

## The scenario

The example this lesson uses is in the repository:
[`examples/mathx`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx).
It is a small C++17 library with one class, a two-dimensional vector, built by
its own `Makefile`; nothing in it mentions cppgo. Every command in this lesson is
one of that `Makefile`'s targets, so you can run them while you read.

## Step 1 — Look at the C++

Here is the header the lesson binds,
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp).
Nothing in it knows that `cppgo` exists: no annotations, no `extern "C"` block,
no binding declarations.

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

Three things in this header show up in the Go API you will get, and it is worth
knowing them before you generate:

- `scale` is `noexcept`, so `Scale` in Go returns nothing: there is no error to
  check on a call that cannot fail.
- `norm`, `at` and `validate` can throw, so their Go methods return an error.
  `at` throwing for an index other than 0 or 1 is the failure this lesson uses at
  the end.
- `dot` takes a pointer and a length, which is how Go passes a slice, so `Dot`
  takes a `[]float64` and the data is not copied.

The definitions are in
[`examples/mathx/src/vector.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/src/vector.cpp).

## Step 2 — Build the library the way you always would

The C++ side is an ordinary shared library, built by its own build system with
its own flags:

```console
$ make lib
g++ -std=c++17 -O2 -fPIC -Wall -Wextra -Iinclude -shared \
    -Wl,-soname,libmathx.so -o build/libmathx.so src/vector.cpp
```

Keep the `-soname` flag: cppgo records the name `libmathx.so` inside your library
when it links against it.

`cppgo` is not involved in this command, and no generated file is compiled into
this library.

## Step 3 — Tell cppgo how your header compiles

`cppgo gen` needs the include paths and the language standard your compiler uses.
The example states them in a `.clangd` file:

```yaml
# examples/mathx/.clangd
CompileFlags:
  Add: [-std=c++17, -Iinclude]
```

If your project already produces `compile_commands.json` (CMake and Meson do),
pass it instead with `-compile-commands build/compile_commands.json` and cppgo
reads the entry for each header.

Do not skip this because the header compiles: a missing include path produces a
wrong binding rather than an error. When cppgo finds no configuration at all it
says so and assumes one:

```
cppgo: warning: the compilation flags for api.hpp are an assumption
(assumed: -std=c++17 -Iinclude); a parse without the project's real flags can
misread the header
```

## Step 4 — Generate the binding

```console
$ make gen
build/cppgo gen -header include/mathx/vector.hpp -package mathx \
                -library build/libmathx.so -out .
mathx::Vector::Vector(const Vector &)        thunk  cppgo_thunk_1af92e8a16a6     not noexcept: an exception would unwind into Go
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c482bce3e     not noexcept: an exception would unwind into Go
mathx::Vector::at(std::size_t) const         thunk  cppgo_thunk_79aeb346bd10     not noexcept: an exception would unwind into Go
...
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
...
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

The report has one line per declaration it bound, and the last line names the
compiler it used to build the C++ side. What the middle columns mean — and why
one declaration reads `direct` and the rest do not — is
[how it works]({{< ref "explanation/architecture.md" >}}), not something you
need in order to use the package.

Generation also wrote the Go package next to the header's project, plus a small
C++ library that your program loads at run time. Commit the generated sources
with your project; the C++ library is a build output. The full list of files is
in [Architecture]({{< ref "explanation/architecture.md" >}}#the-generated-files),
and the example's `Makefile` shows which ones it ignores.

## Step 5 — Call it from Go

`cppgo gen` turned the header in Step 1 into a Go package. These are the four
declarations from
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp)
that the lesson calls:

```cpp
Vector(double x, double y);
~Vector();
double norm() const;
void scale(double k) noexcept;
```

They arrive as these Go declarations, copied from
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
// NewVector constructs a mathx::Vector.
func NewVector(x float64, y float64) (*Vector, error)

// Norm calls mathx::Vector::norm.
func (v *Vector) Norm() (float64, error)

// Scale calls mathx::Vector::scale.
func (v *Vector) Scale(k float64)

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has no live object.
func (v *Vector) Close() error
```

The package also has two entry points that are not generated from a C++
declaration: `Open` loads the two shared objects, and `CppgoLibraryPath` says
where your library is when it is not beside them.

```go
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
func Open(path string) error

// CppgoLibraryPath overrides where the user's library is loaded from, for a
// deployment that does not keep it beside the shim.
var CppgoLibraryPath string
```

Two rules are visible in that list, and they hold for every generated package:

- **A call that can fail returns an error; a call that cannot does not.** `Norm`
  returns `(float64, error)`; `Scale` returns nothing. There is no error value
  that is always nil.
- **A C++ object is a Go value with a lifetime.** A constructor returns
  `(*Vector, error)` and `Close` releases the object. There is no finalizer:
  call `Close`.

Each declaration also carries a comment naming the C++ declaration it came from,
so you can always trace a generated method back to the header.

The two C++ files load as a pair, so keep them in one directory:

```console
$ cp build/libmathx.so .        # puts your library beside the generated one
```

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

Run against the example's files, it prints:

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
caller does not have to know which C++ mechanism reported it.

The last two lines are the lifetime rule. The first `Close` released the object
and returned nil. The second one finds nothing to release and returns
`cppgo.ErrClosed`.

## Step 6 — Run the tests

The example's test suite does the same thing for every binding, once with cgo
disabled and once under the race detector:

```console
$ make test
CGO_ENABLED=0 go test -count=1 -v .
...
--- PASS: TestConstructorAndAccessors (0.00s)
--- PASS: TestThrowingMethod (0.00s)
--- PASS: TestPointByValue (0.00s)
--- PASS: TestCloseLifetime (0.00s)
PASS
ok  	github.com/Manu343726/cppgo/examples/mathx	0.018s
CGO_ENABLED=1 go test -race -count=1 .
ok  	github.com/Manu343726/cppgo/examples/mathx	1.034s
```

The first pass proves the no-cgo rule: the generated package and the runtime
build and run with `CGO_ENABLED=0`. The second pass needs cgo because the race
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
