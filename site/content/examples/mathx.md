+++
title = "mathx: a class"
description = "The v4 generator on one small class: the whole header bound in one run, the direct and thunk carriers, an inline accessor that reaches the shim, a POD by value, exceptions, and a static member."
weight = 10
+++

**Repository path:** [`examples/mathx/`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx)

**Audience:** you want a complete v4 package beside its C++ header and its
`Makefile`.

`mathx` is a two-double `Vector` with a deliberately small surface. It has a
constructor and destructor, `const` accessors, a slice parameter, a mutator, a
static member, an inline accessor, a POD passed by value, and two members that
throw. The report names a carrier for every one of them.

## The header

The class, with the file's longer comments trimmed:

<span class="filename">examples/mathx/include/mathx/vector.hpp</span>

```cpp
#pragma once

#include <cstddef>

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

    // Defined inside the class body, therefore implicitly inline.
    double x() const { return x_; }

    double y() const;
    double norm() const;
    double dot(const double *ys, std::size_t n) const;

    // Throws std::out_of_range for an index other than 0 or 1.
    double at(std::size_t index) const;

    // Throws std::runtime_error for the zero vector.
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

Nothing in it knows that cppgo exists. There is no annotation, no `extern "C"`
block and no binding declaration. `x()` has no out-of-line symbol because its body lives
in the class. `scale()` is the one member declared `noexcept`.

## Generate

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

Read the reasons. `scale` is the only direct binding because it is the only
member declared `noexcept` and defined out of line. Everything else is a thunk. That includes
the inline `x`, the throwing `norm`/`at`/`validate`, the by-value `midpoint`,
and the slice function `saxpy`.

## The generated API

```go
func Open(path string) error
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Close() error
func (v *Vector) Clone() (*Vector, error)
func (v *Vector) At(index uint64) (float64, error)
func (v *Vector) Dot(ys []float64) (float64, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
func (v *Vector) Validate() error
func (v *Vector) X() (float64, error)
func (v *Vector) Y() (float64, error)
func VectorDims() (int32, error)
func Midpoint(a Point, b Point) (Point, error)
func Saxpy(a float64, x []float64, y []float64) error
```

Three decisions worth noticing:

- **`Scale` is the only method with no error.** It is `noexcept`, so the call is
  direct and cannot fail.
- **`Dims` is `VectorDims`**, a package function. A static member has no `this`.
- **`Point` is a Go struct with the same two fields.** A 16-byte POD with a
  natural layout mirrors directly, and the generated file asserts its size,
  alignment and offsets at compile time.

## The call site

From the example's tests:

```go
v, err := NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

n, err := v.Norm()          // 5
d, err := v.Dot([]float64{2, 1}) // 3*2 + 4*1 = 10
v.Scale(2)
x, err := v.X()             // 6
y, err := v.Y()             // 8

mid, err := Midpoint(Point{X: 0, Y: 0}, Point{X: 4, Y: 8}) // {2 4}

if _, err := v.At(2); err != nil {
	// cppgo: mathx::Vector::at: Vector::at: index out of range
}
if err := v.Validate(); err != nil {
	// cppgo: mathx::Vector::validate: Vector::validate: the zero vector has no direction
}
```

The two last calls are the exception path. The C++ function throws, the thunk
catches, and the message arrives as a `*cppgo.CallError` naming the declaration.
The value and the error are ordered value first, error last.

## What it teaches

- **The header is the API.** One run bound the constructor, the destructor, the
  copy constructor, every accessor, the slice method, the mutator, the static
  member, the POD function and the BLAS-shaped free function. The Go side
  declared nothing.
- **The carrier follows the declaration.** `noexcept` and out of line means
  direct. An inline body, a possible throw or a record by value means a thunk.
- **An inline body is supported.** The shim includes the header, so `X()` is
  reachable even though the library exports no symbol for it.
- **A static member is a package function.** The package exposes `VectorDims()`.
  There is no `v.Dims()` method.

## Numbers

This page quotes no benchmark numbers for the example. They depend on your
machine and your compiler. The project-level numbers for the crossing itself,
with the clock caveat, are on [benchmarks]({{< ref "project/benchmarks.md"
>}}). The `stats` example next door is the one with bulk data and direct calls.

Next is [stats]({{< ref "examples/stats.md" >}}).
