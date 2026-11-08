+++
title = "Quickstart"
description = "One class, end to end: a C++ header and its out-of-line definitions, one cppgo gen command, the Go package it generates, and a call site that prints the answer."
weight = 20
+++

This page binds one real C++ class from scratch. Every file below is complete.
copy them into a directory and the last command prints `5` and `10`.

**Audience:** your first binding. You need Go, libclang and a C++ compiler as
described in [installation]({{< ref "manual/installation.md" >}}).

## 1. The C++ library

Two files. A header that *declares*, and a source that *defines*. The split is
not cosmetic, a member defined inside the class definition is implicitly inline,
and the compiler has no obligation to emit a callable symbol for it. cppgo can
still bind it (through a thunk), but an out-of-line `noexcept` member is the one
that takes the fast path.

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
    void scale(double k) noexcept;

private:
    double x_, y_;
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

void Vector::scale(double k) noexcept {
    x_ *= k;
    y_ *= k;
}

} // namespace mathx
```

Build it with your own command. `-Wl,-soname` is what lets the cppgo-built shim
name the library instead of a build path:

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Wl,-soname,libmathx.so \
      -Iinclude -o build/libmathx.so src/vector.cpp
```

## 2. Generate the Go package

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx::Vector::Vector(double, double)   thunk  cppgo_thunk_7d8c482bce3e   not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept   direct _ZN5mathx6Vector5scaleEd    noexcept, out of line, not virtual, register class
mathx::Vector::~Vector()                thunk  cppgo_thunk_06a8bef46cac   not noexcept: an exception would unwind into Go
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

Read the report as three answers:

- **`mathx::Vector::scale(double) noexcept`** — the declaration libclang found,
  with the qualifiers that decide the carrier.
- **`direct`** — the generated trampoline calls the mangled symbol
  `_ZN5mathx6Vector5scaleEd` in your library. The constructor is `thunk`
  because it omits `noexcept`. An exception has to be caught in a C++ frame.
- **the reason** — why that carrier. `noexcept, out of line, not virtual,
  register class` is the direct carrier's recipe.

Because the header is the unit of selection, the package contains the whole
class. `-bind` and `-exclude` narrow
it when a header is too large. See [the generation
input]({{< ref "manual/manifest.md" >}}).

## 3. The Go package you got

```console
$ ls mathx
cppgo_shim  libcppgo_mathx.so  zz_cppgo.gen.go  zz_cppgo_amd64.s
$ ls mathx/cppgo_shim
zz_cppgo_shim.cpp  zz_cppgo_shim.h
```

- `zz_cppgo.gen.go` is the API: types, `NewVector`, methods, `Close`, `Open`.
- `zz_cppgo_amd64.s` has one `NOSPLIT` trampoline per binding.
- `cppgo_shim/` holds the generated C++ thunks. It is cppgo's source, not
  yours. The Go tool ignores it because it is one directory down.
- `libcppgo_mathx.so` is the shim cppgo compiled and linked against the
  library.

The declarations, bodies omitted:

```go
func Open(path string) error

func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Close() error
func (v *Vector) Clone() (*Vector, error)
func (v *Vector) Dot(ys []float64) (float64, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
```

Three shapes are worth noticing:

- **`NewVector` is a function, `Close` is a method.** The destructor is always
  `Close() error`. A second `Close` and a nil receiver return
  `cppgo.ErrClosed`, never a use-after-free.
- **`Scale` has no error, `Norm` has one.** `scale` declares `noexcept`. The
  call is direct and cannot fail. `norm` declares no `noexcept`, so the thunk
  catches what it throws and the signature carries the error. The rule comes
  from the C++ declaration.
- **`Close` and `Norm` return errors a zero value can produce** — a closed
  object and a C++ failure respectively.

## 4. Call it

<span class="filename">main.go</span>

```go
package main

import (
	"fmt"
	"log"

	"example.com/mathxdemo/mathx"
)

func main() {
	// The shim cppgo compiled. Open("") uses the name the package was
	// generated with and leaves the search to the loader.
	if err := mathx.Open("./mathx/libcppgo_mathx.so"); err != nil {
		log.Fatal(err)
	}

	v, err := mathx.NewVector(3, 4)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	n, err := v.Norm()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(n) // 5

	v.Scale(2)

	n, err = v.Norm()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(n) // 10
}
```

```console
$ cp build/libmathx.so ./mathx/     # the shim names it through DT_NEEDED
$ go run .
5
10
```

No cgo in the build:

```console
$ CGO_ENABLED=0 go build -o mathxdemo .
$ ./mathxdemo
5
10
```

The binary is dynamically linked because it loads `libmathx.so` at run time, but
it needed no C toolchain to build, and it contains no `import "C"`.

<div class="callout callout-warn">
<span class="callout-title">Keep the two objects together</span>
<p>
The shim is linked with <code>-Wl,-rpath,$ORIGIN</code> and names your library by
soname, and the default deployment is "the user's library beside the shim". If you
keep it elsewhere, set <code>CPPGO_LIBRARY_PATH</code> or
<code>mathx.CppgoLibraryPath</code> before the first call. A missing shim is an
<code>Open</code> error naming the file. A missing library is the loader naming
the soname.
</p>
</div>

## 5. Make regeneration a check

The generated files are ordinary Go and assembly sources. Commit them, and let
CI prove they are current:

```console
$ cppgo check -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
./mathx: generated files are up to date
```

`check` writes and builds no artefacts. It regenerates the sources in memory
and compares them byte for byte. See [cppgo check in
CI]({{< ref "manual/ci.md" >}}).

## Where to go next

- Every generation flag, including `-bind`, `-instantiate`, `-rename` and
  `-status`: [the generation input]({{< ref "manual/manifest.md" >}}).
- What each generated file contains is in [generated
  code]({{< ref "reference/generated-code.md" >}}).
- For a refused construct, read [when generation
  refuses]({{< ref "tutorials/when-generation-refuses.md" >}}).
