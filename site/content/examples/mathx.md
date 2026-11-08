+++
title = "mathx: a class"
description = "The v4 generator on one small class: the whole header bound in one run, constructor and Close, const members, a noexcept member that takes the direct carrier, an inline member that used to be refused, and a static member."
weight = 10
+++

**Repository path:** [`examples/mathx/`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx)

**Audience:** you want to see a complete v4 package beside its C++ header.

<div class="callout">
<span class="callout-title">This package is being regenerated</span>
<p>
The <code>examples/</code> directory on <code>main</code> still holds the v3
package (Go declarations with directives). Issue #4 regenerates it against the
v4 emitter and updates its <code>Makefile</code>. The report and the generated
API on this page come from the v4 generator run against the header below, not
from the committed v3 package.
</p>
</div>

`mathx` is a two-double `Vector` with a deliberately small surface. A
constructor and destructor, `const` accessors, a slice parameter, a mutator, a
static member, and one member defined inside the class body. That last one is the
interesting difference from v3: v4 **binds** it, through a thunk.

## The header

The class, with the file's longer comments trimmed:

<span class="filename">examples/mathx/include/mathx/vector.hpp</span>

```cpp
#pragma once

#include <cstddef>

namespace mathx {

class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    double x() const;
    double y() const;
    double norm() const;

    double dot(const double *ys, std::size_t n) const;

    void scale(double k);

    static int dims();

    // Defined inside the class definition, therefore implicitly inline.
    double norm_squared() const { return x_ * x_ + y_ * y_; }

private:
    double x_;
    double y_;
};

} // namespace mathx
```

Everything except `norm_squared` is declared here and defined out of line in
`src/vector.cpp`. `norm_squared` has no out-of-line symbol. The generated shim includes the header. The body is compiled into the shim, and the call reaches it.

## Generate

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Wl,-soname,libmathx.so \
      -Iinclude -Iinclude/mathx -o build/libmathx.so src/vector.cpp
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx::Vector::Vector(const mathx::Vector &) thunk  cppgo_thunk_91550e9d2148     no mangled symbol
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c482bce3e     not noexcept: an exception would unwind into Go
mathx::Vector::dims()                        thunk  cppgo_thunk_f4eaab93874e     not noexcept: an exception would unwind into Go
mathx::Vector::dot(const double *, std::size_t) const thunk  cppgo_thunk_9e4411d8977c     not noexcept: an exception would unwind into Go
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::Vector::norm_squared() const          thunk  cppgo_thunk_e20f0cad04a6     not noexcept: an exception would unwind into Go
mathx::Vector::scale(double)                 thunk  cppgo_thunk_768b5ea045a0     not noexcept: an exception would unwind into Go
mathx::Vector::x() const                     thunk  cppgo_thunk_00064a4bc112     not noexcept: an exception would unwind into Go
mathx::Vector::y() const                     thunk  cppgo_thunk_250797b5199d     not noexcept: an exception would unwind into Go
mathx::Vector::~Vector()                     thunk  cppgo_thunk_06a8bef46cac     not noexcept: an exception would unwind into Go
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

Read the reasons. `norm_squared` is a thunk because the library exports no
symbol for an inline body. `scale` is a thunk because the header omits `noexcept`. A v4 package whose header marked `scale` `noexcept` would report it
`direct` instead, the carrier follows the declaration.

The copy constructor reports `no mangled symbol` because the example defines no
copy constructor and the compiler exports no symbol for the implicit one. `Clone` still
works. The thunk copies the object as C++.

## The generated API

```go
func Open(path string) error
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Close() error
func (v *Vector) Clone() (*Vector, error)
func (v *Vector) Dot(ys []float64) (float64, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) NormSquared() (float64, error)
func (v *Vector) Scale(k float64) error
func (v *Vector) X() (float64, error)
func (v *Vector) Y() (float64, error)
func VectorDims() (int32, error)
```

Two decisions worth noticing:

- **`Dims` is `VectorDims`**, a package function. A static member has no `this`, and the result is a package function.
- **`NormSquared` exists.** In v3 this was the example's counter-example. An
  inline member the generator refused. In v4 it is an ordinary thunk-carried
  method, and the package contains no hole where the member used to be.

## The call site

From the generated package, run end to end:

```go
if err := mathx.Open("./mathx/libcppgo_mathx.so"); err != nil {
	log.Fatal(err)
}
v, err := mathx.NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

n, err := v.Norm()          // 5
d, err := v.Dot([]float64{2, 1}) // 3*2 + 4*1 = 10
if err := v.Scale(2); err != nil {
	log.Fatal(err)
}
x, err := v.X()             // 6
sq, err := v.NormSquared()  // 6*6 + 8*8 = 100
```

```console
$ go run .
5 <nil>
10 <nil>
6 <nil>
100 <nil>
```

The full program printed more than the excerpt (it also printed the two
`error` values and `VectorDims` → `2 <nil>`). The numbers are the ones above.

## What it teaches

- **The header is the API.** One run bound the constructor, the destructor, the
  copy constructor, every accessor, the slice method, the mutator, the static
  member and the inline member. The Go side declared nothing.
- **The carrier follows the C++ declaration.** This header declares `scale` without `noexcept`. The method therefore returns an error. The header decides it. Marking it `noexcept` moves it to the direct carrier and
  removes the error.
- **An inline body is supported.** The shim includes the header, and `NormSquared` is reachable.
- **A static member is a package function.** The package exposes `VectorDims()`, and there is no `v.Dims()` method.
- **A slice crosses as (pointer, length).** `Dot` takes `[]float64`. The header
  already declares the `const double *` and the `std::size_t`, and the wrapper
  pairs them.

## Numbers

This page quotes no benchmark numbers for the example. They depend on your
machine and your compiler. The project-level numbers for the crossing itself, 
with the clock caveat, are on [benchmarks]({{< ref "project/benchmarks.md"
>}}). The `stats` example next door is the one with bulk data and a status code.

Next is [stats]({{< ref "examples/stats.md" >}}).
