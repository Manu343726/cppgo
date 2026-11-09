---
title: "Generated API"
description: "The Go shapes cppgo gen writes for records, classes, functions and templates, and the C++ declarations they come from."
weight: 20
---

This page describes the package `cppgo gen` writes. Every shape is shown with
the C++ declaration it comes from — copied from the header in the repository,
linked — and the Go it became, copied from a generated package in the examples.
The C++ is the input; the Go is the output.

## Package entry

Every generated package has one way in. These two are not generated from a C++
declaration: `Open` loads the two shared objects and `CppgoLibraryPath` says
where the user's library is.

```go
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
//
// path names the shim cppgo compiled beside this package. A path of "" uses the
// name the package was generated with. It is safe to call Open more than once;
// the first successful call wins.
func Open(path string) error

// CppgoLibraryPath overrides where the user's library is loaded from, for a
// deployment that does not keep it beside the shim. It must be set before the
// first Open: the two objects are loaded once, and a later change is not
// observed.
var CppgoLibraryPath string
```

The generated methods call `Open("")` themselves when the package is not open
yet, so a program that keeps the two files where the package can find them needs
no explicit call. A missing binding is reported once, as
`the shim cppgo compiled is stale; run cppgo gen again`.

## Records

### A record with a natural layout becomes a Go struct

A C++ record whose fields are all public and whose layout matches Go's becomes a
Go struct with the same fields. This is `mathx::Point`, in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
struct Point {
    double x;
    double y;
};
```

and this is the Go struct it became, from
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
// Point is mathx::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.
type Point struct {
	X float64
	Y float64
}

// NewPoint returns a zero Point.
func NewPoint() (*Point, error)

// Clone copies the value.
func (p *Point) Clone() (*Point, error)
```

A `Point` is passed and returned by value. A record that does not qualify
becomes an opaque value instead: Go storage the C++ side owns, with `Close`,
`Clone` when the C++ type is copyable, and generated accessors for the public
fields.

### A class becomes a struct with a lifetime

The class the generated package in the examples binds, `mathx::Vector`, from
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
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
```

becomes a Go struct with a lifetime:

```go
// Vector is mathx::Vector. The object belongs to C++; obj is where it lives.
type Vector struct {
	obj *cppgo.Object
}

// NewVector constructs a mathx::Vector.
func NewVector(x float64, y float64) (*Vector, error)

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has no live object.
func (v *Vector) Close() error
```

The rules that hold for every class:

- `New<Type>` is a constructor. Constructor overloads carry the same parameter
  suffix as functions, applied to `New<Type>`.
- `Close` releases the object. There is no finalizer, so closing is the caller's
  job.
- `Clone` is the copy constructor. A move constructor is not exposed.
- A static member with no `this` becomes a package function named after its type:
  `func VectorDims() (int32, error)` is `mathx::Vector::dims()`.
- Accessors keep their names: `X`, `Y`, `Norm`, `Dot`, `At`, `Validate`,
  `Scale`.

### Zero and closed values

A method on a nil or closed value returns `cppgo.ErrClosed`. The C++ side is the
inline accessor and the `noexcept` setter in the class above:

```cpp
double x() const { return x_; }

void scale(double k) noexcept;
```

`X` has an error result to carry that failure:

```go
func (v *Vector) X() (float64, error) {
	if v == nil || v.obj == nil {
		return 0, cppgo.ErrClosed
	}
	if err := cppgoOpen(""); err != nil {
		return 0, err
	}
	cppgo.ProbeStack()
	var cppgoResult float64
	cppgoStatus := cppgoCall_00064a4bc112(cppgoSym_00064a4bc112, v.cppgoAddr(), unsafe.Pointer(&cppgoResult))
	cppgo.KeepAlive(v, &cppgoResult)
	if cppgoStatus != 0 {
		return 0, cppgoCallError("mathx::Vector::x")
	}
	return cppgoResult, nil
}
```

A method whose declaration cannot fail has no error result to return, so it
panics instead. This is the generated `Scale`, unchanged:

```go
func (v *Vector) Scale(k float64) {
	if v == nil || v.obj == nil {
		panic("cppgo: mathx::Vector::scale(double) noexcept on a closed Vector")
	}
	if err := cppgoOpen(""); err != nil {
		panic(err)
	}
	cppgo.ProbeStack()
	cppgoCall_5e3a9d471306(cppgoSym_5e3a9d471306, v.cppgoAddr(), k)
	cppgo.KeepAlive(v)
}
```

The difference follows from the signature, not from a preference: `scale` cannot
fail, so its Go signature has nowhere to put an error.

## Functions and overloads

A free function becomes a package function. An overload takes a suffix built
from the Go parameter types, in order, and one declaration keeps the bare name.
These are the declarations, in
[`internal/gen/testdata/overloads/overloads.hpp`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/overloads/overloads.hpp):

```cpp
namespace over {

class Thing {
public:
    Thing();
    Thing(int32_t value);
    ~Thing();

    int32_t value() const;
    int32_t value(int32_t scale) const;

    void reset();
    void reset(int32_t value);

private:
    int32_t value_;
};

int32_t add(int32_t a, int32_t b) noexcept;
double add(double a, double b) noexcept;

} // namespace over
```

and these are the Go functions they became, from
[`internal/gen/testdata/golden/overloads/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/golden/overloads/zz_cppgo.gen.go):

```go
func AddFloat64Float64(a float64, b float64) float64
func AddInt32Int32(a int32, b int32) int32
func NewThing() (*Thing, error)
func NewThingInt32(value int32) (*Thing, error)
```

A collision that no suffix separates is a generation error naming both
declarations, and `-rename '<signature>=<GoName>'` is the way out.

A default argument is **not** expanded by this build: the Go function takes every
parameter of the declaration and the caller passes the value.

## Values: the type map

| C++ | Go | Notes |
|---|---|---|
| `bool` | `bool` | |
| `char`, `signed char`, `unsigned char` | `int8`, `uint8` | |
| `short`, `int`, `long`, `long long` and unsigned | `int16`/`int32`/`int64`, unsigned alike | LP64 |
| `std::int8_t` … `std::uint64_t` | the same width | |
| `std::size_t`, `std::ptrdiff_t` | `uint64`, `int64` | |
| `float`, `double` | `float32`, `float64` | |
| `T *`, `T &` | `*T` | borrowed |
| `(const T *p, std::size_t n)` | `[]T` | borrowed for the call, nothing copied. `T` other than `char`; see below |
| `std::string` | `string` | copied in and out |
| `std::string_view` | `string` | borrowed for the call |
| `std::vector<T>` | `[]T` | copied in and out |
| `std::span<T>` | `[]T` | borrowed |
| `std::array<T,N>` | `[N]T` | by value |
| `std::optional<T>` as a parameter | `*T` | nil is `nullopt` |
| `std::optional<T>` as a result | `*T` | nil means absent, and absence is not an error |
| `T[N]` as a record field | `[N]T` | fixed-size array |
| function pointer, `std::function<R(Args…)>` | `func(…) R` | see *Callbacks* |

For a header that uses them, the fixture in
[`internal/gen/testdata/std/std.hpp`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/std/std.hpp)
declares one function per shape:

```cpp
namespace stdx {

std::string describe(const std::string &prefix, std::string_view suffix);

std::size_t text_length(const std::string &value);

std::string_view literal_view();

std::vector<double> doubled(const std::vector<double> &values);

double sum(std::span<const double> values);

std::array<double, 3> triple(std::array<double, 3> values);

std::optional<double> maybe(bool present, double value);

double unwrap(std::optional<double> value, double fallback);

} // namespace stdx
```

The generated signatures are, copied from
[`internal/gen/testdata/golden/std/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/golden/std/zz_cppgo.gen.go):

```go
func Describe(prefix string, suffix string) (string, error)
func TextLength(value string) (uint64, error)
func LiteralView() (string, error)
func Doubled(values []float64) ([]float64, error)
func Sum(values []float64) (float64, error)
func Triple(values [3]float64) ([3]float64, error)
func Maybe(present bool, value float64) (*float64, error)
func Unwrap(value *float64, fallback float64) (float64, error)
```

Some shapes are refused rather than guessed — `long double`, `__int128`,
`nullptr_t`, `void *`, `std::unique_ptr`/`std::shared_ptr`, a
`non-const` reference to a standard-library value, and every enum. The
[limits page]({{< ref "explanation/limits.md" >}}) has the messages and the
tests.

**A `char *` parameter does not bind in this build.** `const char *` is mapped to
`*int8`, and generation fails with `invalid conversion from 'const int8_t*
{aka 'const signed char*'} to 'const char*'` — with or without a paired length.
Use `std::string` or `std::string_view` at the boundary until the mapping is
fixed.

## Callbacks

A parameter that takes a function pointer or a `std::function` becomes a Go
function value. The limits:

- the call carries at most **six integer and eight floating-point arguments**,
  and one result. A callback that needs more is a generation error naming the
  argument;
- argument and result types are scalars, and pointers to them. A `std::string`
  parameter or a record passed by value in a callback is a generation error;
- a panic inside the callback does not reach C++: the result is zeroed and the
  panic is re-raised in Go when the foreign call returns;
- a callback that has to report a failure writes a status and the C++ side checks
  it. An exception raised across a callback into Go terminates the process.

There is no example in `examples/` that passes a callback yet.

## Templates

A class template — `box::Box` in
[`internal/gen/testdata/box/box.hpp`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/box/box.hpp):

```cpp
template <typename T>
class Box {
public:
    Box() : value_() {}
    Box(T value) : value_(value) {}
    Box(const Box &other) = delete;
    ~Box() {}
    T get() const { return value_; }
    void set(T value) { value_ = value; }
    T add(T other) const { return value_ + other; }
    T *echo(T *value) const { return value; }
    const T *peek(const T *value) const { return value; }
    void consume(const T &value) { value_ = value; }
    static T identity(T value) { return value; }

private:
    T secret() const { return value_; }
    T value_;
};
```

— becomes a Go generic type, and the concrete C++ types are provided by
`cppgo build -scan`:

```go
// Box is box::Box<T>: the generic API. The concrete specializations are the ones
// the program instantiates; `cppgo build` collects them and emits the shim.
type Box[T any] struct {
	obj *cppgo.Object
}

// NewBox constructs a box::Box<T>.
//
// C++: box::Box::Box<T>(T), two-phase template
func NewBox[T any](value T) (*Box[T], error)

// Get calls box::Box::get.
func (b *Box[T]) Get() (T, error)

// Set calls box::Box::set.
func (b *Box[T]) Set(value T) error
```

See [Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## Names

| C++ | Go |
|---|---|
| `snake_case`, `camelCase`, `PascalCase` | `CamelCase`, split on `_` |
| constructor | `New<Type>` |
| destructor | `Close` |
| copy constructor | `Clone` |
| static member `f` | `<Type>F` |
| overload | `<Name><GoTypeOfEachParameter>` |

A C++ name that maps to an existing Go name and cannot be separated by a suffix
is a generation error asking for `-rename`. A `-rename` that matches nothing is
also an error.

## The comment on every binding

Every generated declaration carries a comment naming the C++ declaration it came
from and how cppgo calls it. For `mathx::Vector::at`, declared in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
double at(std::size_t index) const;
```

the generated comment is:

```
// C++: mathx::Vector::at(std::size_t) const, _ZNK5mathx6Vector2atEm (thunk cppgo_thunk_79aeb346bd10, not noexcept: an exception would unwind into Go), verified against libmathx.so
```

It is the first thing to read when a generated method surprises you. The fields
are explained in [Architecture]({{< ref "explanation/architecture.md" >}}#generation-parse-selection-determinism).

## Related

- How to produce this package: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- How a call is made and what it costs: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- The runtime the package imports: [Runtime]({{< ref "reference/runtime.md" >}}).
