---
title: "Bind your first C++ class"
description: "From a C++ header to a passing Go test, and what each generated file is for."
weight: 10
---

This lesson takes one small C++ class from a header to a Go test that calls it.
It assumes you write Go and have never built a foreign function interface, so it
defines each term the first time it uses one.

You will not write a binding by hand. You will point `cppgo` at the header and at
the way the project already compiles it, and it will write a Go package that
calls the class.

## What you need

| Tool | Why | Version |
|---|---|---|
| Go | builds the generated package | 1.25 or newer (`go.mod` declares `go 1.25.0`) |
| A C++ compiler | `cppgo` compiles its shim with it | C++17; the example uses `g++` |
| libclang | `cppgo gen` parses the header with it | **20 or newer** |
| `make` | runs the example's own build | any |

libclang is a *generation-time* dependency only. The program you ship does not
link it and does not need it installed. The reason for the version floor is
concrete: `cppgo` reads the offset of base-class subobjects through
`clang_getOffsetOfBase`, which libclang 19 does not export, so a tree built
against 19 fails at load time with `undefined symbol:
clang_getOffsetOfBase`. The project's CI image pins clang 22
(`ARG CLANG_VERSION=22` in the
[Dockerfile](https://github.com/Manu343726/cppgo/blob/main/Dockerfile)).

The example below is in the repository:
[`examples/mathx`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx).
Every command in this lesson is one of its `Makefile` targets, so you can run
them while you read.

## Step 1 — Look at the C++

Here is the header the lesson binds. Nothing in it knows that `cppgo` exists:
no annotations, no `extern "C"` block, no binding declarations.

```cpp
// examples/mathx/include/mathx/vector.hpp
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

    double x() const { return x_; }          // defined in the class body
    double y() const;
    double norm() const;                     // not noexcept
    double dot(const double *ys, std::size_t n) const;
    double at(std::size_t index) const;      // throws std::out_of_range
    void validate() const;                   // throws std::runtime_error
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

Three properties of this header decide everything that follows:

- `scale` is `noexcept` and defined out of line, so it can be called directly.
- `x`, `y`, `norm`, `at` and the constructors can throw or are inline, so they
  need a C++ frame around the call — the *shim*.
- `dot` takes a pointer and a length. That is exactly what a Go slice is on the
  wire, so the generated signature takes a `[]float64` and copies nothing.

The definitions are in
[`examples/mathx/src/vector.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/src/vector.cpp).
`at` throws for an index other than 0 or 1, and that is the deliberate failure
path this lesson uses at the end.

## Step 2 — Build the library the way you always would

The C++ side is an ordinary shared library, built by its own build system with
its own flags:

```console
$ make lib
g++ -std=c++17 -O2 -fPIC -Wall -Wextra -Iinclude -shared \
    -Wl,-soname,libmathx.so -o build/libmathx.so src/vector.cpp
```

A *shared object* (`.so`) is a library the operating system loads at run time.
The `-soname` flag records the name `libmathx.so` inside the file, and that name
is what other objects use to refer to it. `cppgo`'s shim will link against that
name, so leave it in place.

`cppgo` is not involved in this command, and no generated file is compiled into
this library.

## Step 3 — Tell cppgo how your header compiles

`cppgo gen` parses the header with libclang, a C++ parser library. A parser only
sees the program the *compiler* would see if it is given the same flags, so the
include paths and the language standard have to reach it. The example states
them in a `.clangd` file, the same file clangd reads:

```yaml
# examples/mathx/.clangd
CompileFlags:
  Add: [-std=c++17, -Iinclude]
```

If your project already produces `compile_commands.json` (CMake and Meson do),
pass it instead with `-compile-commands build/compile_commands.json` and cppgo
reads the entry for each header.

This step is not optional bookkeeping. If the include path is wrong, libclang
does not fail: it parses a header in which the declaration you meant is missing,
and the binding that comes out calls something else. cppgo rebases the relative
paths in a compilation database against the entry's own directory for that
reason, and a test pins it (`TestRebaseArgs` in `internal/gen`).

## Step 4 — Generate the binding

```console
$ make gen
build/cppgo gen -header include/mathx/vector.hpp -package mathx \
                -library build/libmathx.so -out .
mathx::Vector::Vector(const Vector &)        thunk  cppgo_thunk_1af92e8a16a6     not noexcept: an exception would unwind into Go
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c482bce3e     not noexcept: an exception would unwind into Go
mathx::Vector::at(std::size_t) const         thunk  cppgo_thunk_79aeb346bd10     not noexcept: an exception would unwind into Go
mathx::Vector::dims()                        thunk  cppgo_thunk_f4eaab93874e     not noexcept: an exception would unwind into Go
mathx::Vector::dot(const double *, std::size_t) const thunk  cppgo_thunk_9e4411d8977c     not noexcept: an exception would unwind into Go
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::validate() const              thunk  cppgo_thunk_2ceca75f5a00     not noexcept: an exception would unwind into Go
mathx::Vector::x() const                     thunk  cppgo_thunk_00064a4bc112     not noexcept: an exception would unwind into Go
mathx::Vector::y() const                     thunk  cppgo_thunk_250797b5199d     not noexcept: an exception would unwind into Go
mathx::Vector::~Vector()                     thunk  cppgo_thunk_06a8bef46cac     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
mathx::saxpy(double, const double *, double *, std::size_t) thunk  cppgo_thunk_edd159695f62     not noexcept: an exception would unwind into Go
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

Four columns: the C++ declaration, its **carrier**, the symbol, and the reason.
A *carrier* is how one call crosses the boundary, and every declaration gets
exactly one:

- **direct**: the generated assembly calls the C++ symbol the library exports.
  It is the fast path, and it is only allowed when the declaration is
  `noexcept`, not virtual, defined out of line, and its arguments fit the
  processor registers. `scale` is the one line here.
- **thunk**: a small `extern "C"` function that `cppgo` writes, compiles into
  the shim, and calls the C++ declaration as C++. Everything else takes it.
- **reentrant**: a thunk called through a different runtime entry, used when the
  signature passes a Go function to C++. This header takes no callbacks.

The report is the audit of the API: no declaration is silently dropped, and the
last line names the compiler that built the shim.

## Step 5 — What appeared on disk

| File | Contents |
|---|---|
| `zz_cppgo.gen.go` | the Go API you call, plus a load function and one declaration per trampoline |
| `zz_cppgo_amd64.s` | one NOSPLIT assembly trampoline per binding: it aligns the stack, makes the call, restores it |
| `cppgo_shim/zz_cppgo_shim.h` | the `extern "C"` prototypes of the thunks |
| `cppgo_shim/zz_cppgo_shim.cpp` | the thunks themselves, one per non-direct binding |
| `libcppgo_mathx.so` | the shim, compiled by cppgo and linked against `libmathx.so` |

The first four are sources and the example commits them, because an example is
teaching material and the command in the file banner must reproduce them. The
`.so` is a build artefact and is ignored by git. Nothing here is written into
`build/libmathx.so`.

*NOSPLIT* is a Go assembly attribute: it tells the runtime not to grow the
goroutine's stack at that point, which is what makes it legal to switch to a
16-byte-aligned stack for the C call.

## Step 6 — Read the generated Go

The generated file is ordinary Go. These are the declarations this lesson uses,
copied from
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
func Open(path string) error

// CppgoLibraryPath overrides where the user's library is loaded from, for a
// deployment that does not keep it beside the shim.
var CppgoLibraryPath string

// NewVector constructs a mathx::Vector.
//
// C++: mathx::Vector::Vector(double, double), _ZN5mathx6VectorC1Edd (thunk
// cppgo_thunk_7d8c482bce3e, not noexcept: an exception would unwind into Go),
// verified against libmathx.so
func NewVector(x float64, y float64) (*Vector, error)

// Norm calls mathx::Vector::norm.
func (v *Vector) Norm() (float64, error)

// Scale calls mathx::Vector::scale.
//
// C++: mathx::Vector::scale(double) noexcept, _ZN5mathx6Vector5scaleEd
// (direct, noexcept, not virtual, register class; the library exports the
// symbol), verified against libmathx.so
func (v *Vector) Scale(k float64)

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has
// no live object.
func (v *Vector) Close() error
```

Three rules are visible in that list, and they hold for every generated package:

- **A call that can fail returns an error; a call that cannot does not.** `Norm`
  returns `(float64, error)` because it is not `noexcept`; `Scale` returns
  nothing because it is. There is no error value that is always nil.
- **A C++ object is a Go struct with a real lifetime.** `Vector` wraps storage
  the C++ side owns. A constructor returns `(*Vector, error)`, and `Close` runs
  the destructor. There is no finalizer: call `Close`.
- **The evidence line names the C++ declaration, its mangled symbol and the
  carrier.** That comment is the record of what the call does, and it is what
  you read when a generated method surprises you.

The mangled symbol is the name the C++ compiler gives the function in the
library's symbol table. C++ has overloading and namespaces, so the compiler
encodes the signature into the name; `_ZN5mathx6Vector5scaleEd` is
`mathx::Vector::scale(double)`.

## Step 7 — Call it from Go

The two objects load as a pair. This program keeps both files in one directory
(`cp build/libmathx.so .` puts the library beside the shim), so the shim finds
its sibling through the `$ORIGIN` run path cppgo links it with:

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
	if err := mathx.Open("libcppgo_mathx.so"); err != nil {
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

Run against the example's objects, it prints:

```console
norm = 5, dot = 3
At(2): cppgo: mathx::Vector::at: Vector::at: index out of range
  is a *cppgo.CallError: true
scaled = (6, 8)
close: <nil>
close again: library is closed
```

Read the third and fourth lines together. The C++ `at` threw
`std::out_of_range`; the shim caught it and stored the message; the generated
binding returned it as `*cppgo.CallError` naming the declaration. The Go caller
cannot tell that an exception was involved, which is the point.

The last two lines are the lifetime rule. The first `Close` ran the destructor
and returned nil. The second one finds no live object and returns
`cppgo.ErrClosed`.

## Step 8 — Run the tests

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

Measured 2026-11-08 on the machine that built this tree. The first pass proves
the no-cgo rule: the generated package and the runtime build and run with
`CGO_ENABLED=0`. The second pass needs cgo because the race detector is built on
it; the two properties cannot be measured by one command.

## What you just did

You wrote no binding. You told cppgo where the declarations are and how they are
compiled, and it produced:

1. a Go package that mirrors the C++ API,
2. assembly that makes each call without cgo,
3. a C++ translation unit that carries everything the direct call cannot,
4. the compiled shim that links your library.

Your C++ build produced its library exactly as before, and no generated file is
part of it.

## Where to go next

- Bind your own library: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- Ship the pair: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
- Understand the machinery: [Architecture]({{< ref "explanation/architecture.md" >}}) and the
  [carrier table]({{< ref "reference/carriers.md" >}}).
- Look up a signature: [Generated API]({{< ref "reference/generated-api.md" >}}).
