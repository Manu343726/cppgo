---
title: "Generated API"
description: "The Go shapes cppgo gen writes for records, classes, functions and templates."
weight: 20
---

This page describes the package `cppgo gen` writes. The snippets are copied from
the generated files in the examples and from the generator's golden files, so
they are the output, not a sketch of it.

## Package entry

Every generated package has one way in:

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
yet, so a program that keeps the objects where the package can find them needs no
explicit call. A missing shim symbol is reported once, as
`the shim cppgo compiled is stale; run cppgo gen again`.

## Records

### A mirrorable record becomes a Go struct

A record is mirrorable when all its fields are public, its layout is the natural
Go layout, and no field is a bitfield, a reference, a pointer, a union member or
a non-mirrorable type. Its Go value *is* the C layout, and the generated file
asserts that at compile time:

```go
// Point is mathx::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.
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

// NewPoint returns a zero Point.
func NewPoint() (*Point, error)

// Clone copies the value.
func (p *Point) Clone() (*Point, error)
```

A `Point` crosses by value, and the shim copies it in and out. A record that is
not mirrorable becomes an opaque value instead: a Go struct with unexported
storage, `Close()` from the destructor, `Clone()` from the copy constructor when
there is one, and generated accessors for the public fields.

### A class becomes a struct with a lifetime

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
- `Close` runs the destructor. There is no finalizer, so closing is the caller's
  job.
- `Clone` is the copy constructor. A move constructor stays internal.
- A static member with no `this` becomes a package function named after its type:
  `func VectorDims() (int32, error)` is `mathx::Vector::dims()`.
- Accessors keep their names: `X`, `Y`, `Norm`, `Dot`, `At`, `Validate`,
  `Scale`.

### Zero values are invalid, and behave differently by carrier

A method on a nil or closed value returns `cppgo.ErrClosed`:

```go
func (v *Vector) X() (float64, error) {
	if v == nil || v.obj == nil {
		return 0, cppgo.ErrClosed
	}
	...
}
```

A **direct** binding has no error channel to return, so it panics instead. This
is the generated `Scale`, unchanged:

```go
func (v *Vector) Scale(k float64) {
	if v == nil || v.obj == nil {
		panic("cppgo: mathx::Vector::scale(double) noexcept on a closed Vector")
	}
	...
}
```

That difference follows from the signature, not from a preference: `scale` is
`noexcept`, so its Go signature has no error result to put `ErrClosed` in.

## Functions and overloads

A free or static function becomes a package function. An overload takes a suffix
built from the Go parameter types, in order, and one declaration keeps the bare
name. From the generator's overload fixture:

```go
func AddFloat64Float64(a float64, b float64) float64
func AddInt32Int32(a int32, b int32) int32
func NewThing() (*Thing, error)
func NewThingInt32(value int32) (*Thing, error)
```

A collision that no suffix separates is a generation error naming both
declarations, and `-rename '<signature>=<GoName>'` is the way out.

A default argument is **not** expanded by this build: the Go function takes every
parameter and the caller passes the default. `int add(int a, int b = 2)` becomes
`func Add(a int32, b int32) int32`.

## Values: the type map

| C++ | Go | Notes |
|---|---|---|
| `bool` | `bool` | |
| `char`, `signed char`, `unsigned char` | `int8`, `uint8` | |
| `short`, `int`, `long`, `long long` and unsigned | `int16`/`int32`/`int64`, unsigned alike | LP64, sizes validated against libclang |
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

The shipped signatures, from the standard-library golden file:

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
`*int8`, and the shim then fails to compile with `invalid conversion from
'const int8_t*' {aka 'const signed char*'} to 'const char*'` — with or without a
paired length. Use `std::string` or `std::string_view` at the boundary until the
mapping is fixed.

## Callbacks

A parameter that takes a function pointer or a `std::function` becomes a Go
function value. The binding registers it in the callback table, calls the thunk
through the **reentrant** carrier, and releases the registration when the call
returns:

```go
cppgo.CallReentrant(fn, &cppgoFrame)
```

The limits are the ABI's, not the generator's convenience:

- at most **six INTEGER and eight SSE arguments**, and one register result.
  A callback that needs more is a generation error naming the argument;
- argument and result types are scalars (and pointers to them), plus the shapes
  the register file can carry. A `std::string` parameter or a record passed by
  value in a callback is a generation error;
- a panic inside the callback does not unwind into C++: it is captured, the
  result is zeroed, and the binding re-panics in Go after the call returns;
- a callback that wants to report a failure writes a status into its frame; the
  C++ side checks it. An exception raised across a Go callback frame terminates
  the process.

The mechanism is measured in `internal/callback` and `internal/shim`. There is no
example in `examples/` that passes a callback yet.

## Templates

A class or function template becomes a Go generic type or function, and the
concrete C++ specialisations are collected by `cppgo build -scan`:

```go
type Box[T any] struct{ obj *cppgo.Object }

func NewBox[T any](value T) (*Box[T], error)
func (b *Box[T]) Get() (T, error)
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

## The evidence line

Every binding carries one line naming the C++ declaration and the reason for its
carrier:

```
// C++: mathx::Vector::at(std::size_t) const, _ZNK5mathx6Vector2atEm (thunk cppgo_thunk_79aeb346bd10, not noexcept: an exception would unwind into Go), verified against libmathx.so
```

A direct binding says so instead, and a template says `two-phase template`. This
comment is the record of what the call does; it is also the first thing to read
when a generated method surprises you.

## Related

- How to produce this package: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- What each carrier costs: [Carrier table]({{< ref "explanation/carriers.md" >}}).
- The runtime the package imports: [Runtime]({{< ref "reference/runtime.md" >}}).
