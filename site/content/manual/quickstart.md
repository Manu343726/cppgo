+++
title = "Quickstart"
description = "One class, end to end: a C++ header and its out-of-line definitions, the Go declarations with cppgo directives, the generator run, and a call site that prints the answer."
weight = 20
+++

This page binds one real C++ class from scratch. Every file below is complete;
copy them into a directory and the last command prints `5`.

## 1. The C++ library

Two files: a header that *declares*, and a source that *defines*. The split is
not cosmetic. A member defined inside the class definition is implicitly inline,
emits no dynamic symbol, and cannot be bound in the direct ABI mode.

<span class="filename">include/mathx/vector.hpp</span>

```cpp
#pragma once

#include <cstddef>

namespace mathx {

class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    double norm() const;
    double dot(const double *ys, std::size_t n) const;

private:
    double x_;
    double y_;
};

} // namespace mathx
```

<span class="filename">src/vector.cpp</span>

```cpp
#include "mathx/vector.hpp"

#include <cmath>

namespace mathx {

Vector::Vector(double x, double y) : x_(x), y_(y) {}
Vector::~Vector() {}

double Vector::norm() const { return std::sqrt(x_ * x_ + y_ * y_); }

double Vector::dot(const double *ys, std::size_t n) const {
    const double components[2] = {x_, y_};
    double sum = 0.0;
    if (n > 2) {
        n = 2;
    }
    for (std::size_t i = 0; i < n; ++i) {
        sum += components[i] * ys[i];
    }
    return sum;
}

} // namespace mathx
```

Build it:

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Iinclude -o build/libmathx.so src/vector.cpp
```

## 2. The Go module

```console
$ go mod init example.com/mathxdemo
$ go get github.com/Manu343726/cppgo
```

The bindings live in their own package, because the generator rewrites the files
it reads:

<span class="filename">bindings/mathx/mathx.go</span>

```go
// Package mathx binds mathx::Vector directly, with no extern "C" layer.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ../../../include/mathx/vector.hpp

// Vector mirrors mathx::Vector. A defined type over cppgo.Handle: the
// receiver value *is* the object pointer.
//
//cppgo:class mathx::Vector
type Vector cppgo.Handle

// New constructs a Vector.
//
//cppgo:ctor
func New(x, y float64) (Vector, error)

// Norm returns the Euclidean norm.
//
//cppgo:method norm
func (v Vector) Norm() float64

// Dot returns the dot product with the first n components of ys.
//
//cppgo:method dot
func (v Vector) Dot(ys []float64) (float64, error)

// Destroy destroys the object.
//
//cppgo:dtor
func (v Vector) Destroy()
```

Rules that are already visible here:

- `//cppgo:library` and `//cppgo:header` are package-level, once each.
- `//cppgo:header` is resolved relative to the package directory.
- `//cppgo:class` sits on a `type X cppgo.Handle`; that type is how the
  generator knows `Vector` is a C++ object rather than an integer.
- `//cppgo:ctor`, `//cppgo:dtor`, `//cppgo:method` and `//cppgo:function` are
  written in the doc comment of the Go function they bind.
- The `error` results here are placeholders. In the direct ABI mode a call has
  no status channel, so the generated wrapper documents "the trailing error is
  always nil" and returns `nil`. Errors come from C++ only when it returns an
  integer status (see [error handling]({{< ref "manual/errors.md" >}})).

## 3. Generate

```console
$ cppgo gen -dir ./bindings/mathx
```

The report goes to stdout, one line per binding, ordered by C++ name:

```
mathx::Vector::Vector              mangled _ZN5mathx6VectorC1Edd     abi from fallback flags (assumed); note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::dot                 mangled _ZNK5mathx6Vector3dotEPKdm abi from fallback flags (assumed); note: the trailing error is always nil: a direct C++ call has no status channel (return an int32 status and declare only error to get one)
mathx::Vector::norm                mangled _ZNK5mathx6Vector4normEv  abi from fallback flags (assumed); note: the trailing error is always nil ...
mathx::Vector::~Vector             mangled _ZN5mathx6VectorD1Ev      abi from fallback flags (assumed)
```

Two things to read in that output:

- **`abi from fallback flags (assumed)`** means there was no
  `compile_commands.json` and no `.clangd`, so the header was parsed with
  `-std=c++17 -I<header dir>`. It worked here. On a header with includes of its
  own, point `-compile-commands` at your project's database instead. The
  generator also prints a warning to stderr when it assumes flags.
- **`mangled _ZN5mathx6VectorC1Edd`** is the symbol the trampoline will `dlsym`.
  `C1` is the complete-object constructor; the `K` in `_ZNK...normEv` is the
  `const` qualifier. You never write these by hand.

The files written next to `mathx.go` are:

```
bindings/mathx/mathx.go          rewritten in place: each body now delegates
bindings/mathx/zz_cppgo.gen.go   wrappers, lazy library binding, trampoline declarations
bindings/mathx/zz_cppgo_amd64.s  one NOSPLIT trampoline per bound function
```

Because `-out` defaulted to `-dir`, the input file is rewritten **in place**
rather than copied to `zz_cppgo_stub.go`. That is deliberate: regeneration is
idempotent, the directives stay where you wrote them, and the package keeps
compiling. With `-out ./gen` you get a separate package instead, and the
rewritten source appears as `zz_cppgo_stub.go`.

`mathx.go` now reads:

```go
//cppgo:ctor
func New(x, y float64) (Vector, error) { return cppgoCallNew(x, y) }

//cppgo:method norm
func (v Vector) Norm() float64 { return cppgoCallNorm(v) }

//cppgo:method dot
func (v Vector) Dot(ys []float64) (float64, error) { return cppgoCallDot(v, ys) }

//cppgo:dtor
func (v Vector) Destroy() { cppgoCallDestroy(v) }
```

The directives stay in the doc comments, so `cppgo gen` can be run again from
the rewritten source and will produce the same bytes.

## 4. Call it

<span class="filename">main.go</span>

```go
package main

import (
	"fmt"
	"log"

	"example.com/mathxdemo/bindings/mathx"
)

func main() {
	// The library name given to //cppgo:library is passed to dlopen as
	// written. Point the generated package at the .so you just built; this
	// variable is read once, on the first call.
	mathx.CppgoLibraryPath = "./build/libmathx.so"

	v, err := mathx.New(3, 4)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Destroy()

	fmt.Println(v.Norm())                  // 5
	fmt.Println(v.Dot([]float64{1, 0}))    // 3
}
```

```console
$ go run .
5
3
```

`CppgoLibraryPath` is emitted into every generated package and is the simplest
way to load a library from a relative path. The alternatives are an absolute
path in `//cppgo:library`, `LD_LIBRARY_PATH`, or an `-rpath` on the binary.

<div class="callout callout-warn">
<span class="callout-title">Destroy is not optional</span>
<p>
A constructor allocates storage that the generated package keeps alive in a
registry; the destructor calls <code>D1</code> and drops the registry entry.
There is no finalizer, on purpose: a finalizer would run on an arbitrary
goroutine at an unpredictable time, possibly after the library is closing. An
object that is never destroyed is never reclaimed. Every constructor needs its
destructor, exactly as in C++.
</p>
</div>

## Where to go next

- Every directive, including `//cppgo:function` for free functions and
  `//cppgo:mode shim`: [directives]({{< ref "manual/directives.md" >}}).
- What each generated file contains, line by line: [generated
  code]({{< ref "reference/generated-code.md" >}}).
- What to do when the generator refuses a member: [troubleshooting]({{< ref
  "manual/troubleshooting.md" >}}) and [when the generator
  refuses]({{< ref "tutorials/when-generation-refuses.md" >}}).
