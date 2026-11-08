# cppgo — design contract (v3)

This file is the contract for every workstream. If something is not written here, do not invent it:
ask the orchestrator. Where the tree and this file disagree during the migration it describes, this
file is right; the decisions it replaces are recorded in
[ADR-0001](docs/adr/0001-header-first-bindings.md).

## What this is

`cppgo` calls C++ from Go with **no cgo at build time** and **no `import "C"`**. A generator reads
C++ headers, proves at build time what every binding compiles to, and emits:

- a Go package, the only API the caller sees;
- one `NOSPLIT` assembly trampoline per symbol the Go side calls;
- a C++ translation unit (`zz_cppgo_shim.cpp` + header) with one `extern "C"` thunk per binding
  that the direct ABI cannot express.

Every construct that carries data across the bound API is supported. A construct the direct ABI
cannot call is not refused: it goes through a generated thunk, and the thunk is ordinary C++ that
the user's build compiles into the library. **Completeness is the product; the direct ABI is the
fast path, not the boundary of what can be bound.**

## The two layers

The runtime and the generator are separate products with separate dependencies, and the line between
them is part of this contract.

| layer | package | owns | may depend on |
|---|---|---|---|
| **runtime** | `github.com/Manu343726/cppgo` (repository root) | dynamic loading, symbol resolution, the C ABI and its trampolines, stack probing, foreign-object storage, wire helpers, error types | the standard library, `internal/asm`, `internal/rtl`, `purego` |
| **generator** | `internal/gen`, `internal/cxx`, `cmd/cppgo` | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim | anything, including libclang through `purego` |

The runtime **never imports `internal/cxx`, directly or transitively, and has no libclang
dependency**. libclang is generation-time only. The runtime builds and its tests run with
`CGO_ENABLED=0` on a machine with no libclang installed; the generator needs libclang and says so
with `ErrLibclangNotFound` when it is absent.

What the generated code uses from the runtime, and nothing else:

```go
func Open(path string) (*Library, error)     // purego dlopen/dlsym underneath
func (l *Library) Symbol(name string) (uintptr, error)
func (l *Library) Close() error
func (l *Library) Path() string

func ProbeStack()                            // StackReserve bytes of goroutine stack headroom
const StackReserve = 32 << 10

func NewObject(size, align uintptr, release func()) *Object
                                            // Go storage whose layout Go does not know;
                                            // release runs the concrete destructor
func (o *Object) Addr() uintptr             // pinned, aligned address handed to C++
func (o *Object) Retain()                   // a view shares ownership with the object
func (o *Object) Release()                  // drops one reference; release runs at zero

func Ptr[T any](s []T) unsafe.Pointer       // wire helpers
func StrPtr(s string) unsafe.Pointer
func KeepAlive(...any)
func LastError(lib *Library) error

type Handle uintptr                          // low-level only; never in a generated signature
```

`Object` pins its storage with `runtime.Pinner`, so there is no package-level object registry: the
storage is reachable from the generated Go value that owns it, and it does not move while pinned.

### Runtime additions introduced by this contract

Some of the surface above does not exist in the tree yet. It is **planned API**, not stale text: the
emitter phase adds it, and the contract is what defines it.

| name | layer | status | what it guarantees |
|---|---|---|---|
| `Object`, `NewObject(size, align, release)`, `Addr`, `Retain`, `Release` | runtime | **planned** | a pinned, aligned Go block; a Go-side reference count; the concrete destructor (`release`) runs exactly once, when the last reference goes |
| `*cppgo.CallError{Symbol, Err}` | runtime | **planned** | a failed binding, with the C++ symbol and the native message; wraps `*NativeError` so `errors.As` works |
| `Open(path string) error`, generated in each bound package | generator output | **planned** | loads the library, resolves every direct symbol **and every thunk**, and reports the generated shim file when one is missing |
| `Open`, `Library`, `Symbol`, `Close`, `Path`, `MustSymbol`, `ProbeStack`, `StackReserve`, `Ptr`, `StrPtr`, `KeepAlive`, `LastError`, `OpenError`, `SymbolError`, `NativeError`, `ErrClosed` | runtime | **in the tree** | the loader, the wire helpers and the typed errors as they exist today |
| `Handle` | runtime | **in the tree, not used by generated code** | a low-level uintptr for hand-written callers only |

`Object` serves two shapes of ownership and `release` is what tells them apart: for a value whose
layout libclang could not complete, the shim heap-allocates it and `release` deletes it; for a value
constructed into Go-pinned storage, `release` calls its destructor. The generated Go value never
needs to know which one it holds, and `Close` is correct for both.

## How a binding is carried: direct or thunk

Every binding is assigned exactly one **carrier** before anything is emitted. The generator never
leaves a declaration unbound and never emits a call it has not established a carrier for.

### Direct

The Go trampoline calls the mangled C++ symbol itself. The rules are those the trampoline was
measured on, and all of them must hold:

- the declaration has an out-of-line body and external linkage (a dynamic symbol exists);
- it is **not virtual** (no vtable walk is needed: the call is to a fixed address);
- it is **`noexcept`** (the direct path has no handler, so nothing may escape);
- the signature is register-representable: scalars, enums, pointers and references, a slice/string
  as `(pointer, length)`, and a POD of at most 16 bytes with no MEMORY-class part. No `std::` type
  and no template that has not been instantiated into a normal symbol.

A declaration outside that shape is **not refused**: it goes through a thunk. A >16-byte or
MEMORY-class POD is copied by the thunk as an argument and returned through the sret pointer into Go
storage; a non-POD by value is copied the same way.

### Thunk

Everything else is carried by a generated `extern "C"` thunk in `zz_cppgo_shim.cpp`. The thunk is
C++ compiled against the same headers, with the same flags, inside the user's build:

| what the declaration is | what the thunk does |
|---|---|
| inline / header-only body | includes the header and calls the function: the definition is right there |
| virtual method, virtual destructor | calls it as C++ does — **real dispatch through the vtable**, including through a base pointer or reference |
| template | instantiates the concrete specialisation (`template struct Box<int>;`, `template T max<T>(T,T);`) and calls it |
| `std::string` / `string_view` / `vector` / `span` / `array` / `optional` | constructs the C++ value, converts, copies in or out (see *Type map*) |
| POD above 16 bytes / MEMORY class by value | copies it into the call; for a result, passes the hidden sret pointer aimed at Go storage |
| non-POD by value | copies the Go-held object into the call, destroys it at the end, or placement-news the result into Go storage |
| exception-throwing call | wraps the call in `try`/`catch`, converts `std::exception` and `...` into a `cppgo_last_error()` message |
| multiple inheritance / virtual base | performs the `static_cast`/`dynamic_cast` that adjusts `this`, in C++, where the layout is known |
| more than 6 INTEGER or 8 SSE arguments | takes a pointer to a packed argument block declared by the generated header, unpacks it and calls |
| anything with an out parameter | writes through the pointer the Go side provides |

Both carriers are ordinary C ABI functions from Go's point of view: `Open` resolves a `uintptr`,
`ProbeStack` runs, and one NOSPLIT trampoline calls it. The Go wrapper is generated from the same
signature table either way, so the carrier is invisible in the API and visible in the report:

```
mathx::Vector::norm     thunk  cppgo_thunk_9f2c1a4b7e03   not noexcept
mathx::Vector::scale    direct _ZN5mathx6Vector5scaleEd   noexcept, out of line, register class
mathx::Box<int>::get    thunk  cppgo_thunk_41d0ee9a2c77   inline body, template instantiation
```

### The shim artefacts

- `zz_cppgo_shim.h` — the `extern "C"` declarations, the argument-block structs, and the
  thread-local error API (`cppgo_last_error`) the Go side reads. It is plain C++ and may be
  included by other C++ code.
- `zz_cppgo_shim.cpp` — the definitions, the template instantiations, the casts and the `try`/
  `catch` bodies. It is compiled by the user's normal C++ build into the same library the Go side
  loads. It is not a separate shared object.
- `cppgo gen` parses the shim it is about to write through libclang with the same flags, and
  **fails generation on a diagnostic**; the user's build compiles it for real. A binding whose thunk
  would not compile is a generation error with the compiler's message, never a symbol that fails at
  the first call.
- `Open` resolves **every** symbol the package needs, direct and thunk, before any binding can run.
  A library built without the generated shim fails at `Open` with the missing thunk's name and the
  file to compile.

## The generated API

The input is C++. For this header:

```cpp
namespace mathx {

struct Point { double x, y; };

class Vector {
public:
    Vector(double x, double y);
    Vector(const Vector &other);              // user-declared copy constructor
    ~Vector();
    double x() const;                         // inline in the class
    double norm() const;                      // out of line, can throw
    double dot(const double *ys, std::size_t n) const;
    void scale(double k) noexcept;
    static int dims();
private:
    double x_, y_;
};

struct Tiny { int value; ~Tiny(); };          // non-POD by value
Tiny produce();                               // non-POD by-value result

class Shape {
public:
    virtual ~Shape();
    virtual double area() const;
};

class Circle : public Shape {
public:
    Circle(double r);
    ~Circle() override;
    double area() const override;
    double radius() const;
private:
    double r_;
};

// y[i] = a * x[i] + y[i] for i in [0, n).
void saxpy(double a, const double *x, double *y, std::size_t n);

std::string describe(const std::string &prefix, std::string_view suffix);
std::vector<double> doubled(const std::vector<double> &values);
double sum(std::span<const double> values);
double dot3(std::array<double, 3> a, std::array<double, 3> b);
std::optional<double> maybe_norm(const Vector &v);
std::unique_ptr<Vector> make_vector(double x, double y);
std::shared_ptr<Vector> share(const std::shared_ptr<Vector> &v);
void consume_tiny(Tiny value);

template <typename T>
struct Box { T value; T get() const { return value; } };

} // namespace mathx
```

`cppgo gen` writes this package — this is the API, not an illustration.

### Classes, values and PODs

```go
// Code generated by cppgo gen. DO NOT EDIT.
//
//	cppgo gen -header include/mathx/vector.hpp -package mathx -library libmathx \
//	          -instantiate mathx::Box<int> -out ./mathx

package mathx

import "github.com/Manu343726/cppgo"

// Point is mathx::Point: a POD, 16 bytes, fields at offsets 0 and 8.
type Point struct {
	X float64
	Y float64
}

// Vector is mathx::Vector. The object belongs to C++; obj is where it lives.
type Vector struct {
	obj *cppgo.Object
}

// NewVector constructs a mathx::Vector.
//
// C++: mathx::Vector::Vector(double, double), _ZN5mathx6VectorC1Edd
func NewVector(x, y float64) (*Vector, error) { /* Open, NewObject(16, 8, releaseVector), thunk */ }

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has
// no live object: a nil pointer, the zero value, or a second Close.
//
// C++: mathx::Vector::~Vector(), _ZN5mathx6VectorD1Ev
func (v *Vector) Close() error { /* concrete destructor at the last reference */ }

// Clone copies the object through its copy constructor. Clone is generated
// when the record is classified copy-constructible.
//
// C++: mathx::Vector::Vector(const Vector &), _ZN5mathx6VectorC1ERKS0_
func (v *Vector) Clone() (*Vector, error)

// X returns the x component.
//
// C++: mathx::Vector::x() const, _ZNK5mathx6Vector1xEv
func (v *Vector) X() (float64, error) // not noexcept: thunk, error

// Norm returns the Euclidean norm.
//
// C++: mathx::Vector::norm() const, _ZNK5mathx6Vector4normEv
func (v *Vector) Norm() (float64, error)

// Dot returns the dot product of v with ys. The slice is borrowed for the call.
//
// C++: mathx::Vector::dot(const double *, std::size_t) const, _ZNK5mathx6Vector3dotEPKdm
func (v *Vector) Dot(ys []float64) (float64, error)

// Scale multiplies both components by k. This one is noexcept and register
// representable, so it is called directly and cannot fail.
//
// C++: mathx::Vector::scale(double) noexcept, _ZN5mathx6Vector5scaleEd
func (v *Vector) Scale(k float64)

// VectorDims is the static member mathx::Vector::dims.
//
// C++: mathx::Vector::dims(), _ZN5mathx6Vector4dimsEv
func VectorDims() (int32, error)

// Saxpy computes y[i] = a*x[i] + y[i] in place. x and y share the C++ length
// parameter and must have the same length in Go; a mismatch is an error before
// the call is made.
//
// C++: mathx::saxpy(double, const double *, double *, std::size_t), _Z5saxpydPKdPdm
func Saxpy(a float64, x, y []float64) error

// Tiny is mathx::Tiny: a non-POD value. It owns C++ storage and is copied into
// a call when a function takes it by value.
type Tiny struct {
	obj *cppgo.Object
}

// Close destroys the value. Clone copies it through its copy constructor.
func (t *Tiny) Close() error
func (t *Tiny) Clone() (*Tiny, error)

// Value and SetValue are the generated accessors for the public field value.
// Reading or writing an int cannot throw, so neither carries an error.
func (t *Tiny) Value() int32
func (t *Tiny) SetValue(value int32)

// ConsumeTiny takes mathx::Tiny by value: the thunk copies the Go-held object
// into the call, so t stays valid afterwards.
//
// C++: mathx::consume_tiny(Tiny)
func ConsumeTiny(t *Tiny) error

// Produce returns mathx::Tiny by value: the result is constructed in Go-owned
// storage and returned as an owning value.
//
// C++: mathx::produce(), _ZN5mathx7produceEv
func Produce() (*Tiny, error)
```

Rules that the API shape follows:

- **Mirrorable record → Go struct with matching exported fields.** A record is mirrorable when its
  fields are all public, its layout is the natural Go layout, and no field is a bitfield, a
  reference, a pointer, a union member, or a non-mirrorable type. The generator reads `sizeof`,
  `alignof` and every offset from libclang and asserts them at compile time in the generated file.
- **Every other record → opaque value type**: a Go struct with unexported storage, `Close()` from
  the destructor, `Clone()` from the copy constructor (when one exists), and generated accessors for
  every public field (`Field()` / `SetField(v)`, mapped by the same type map). Bitfields, packed
  layouts, private members, non-POD members, and a layout libclang could not complete all take this
  path; **nothing is left unreachable** and nothing is refused.
- **Members are modelled by kind.** A record carries its public data fields (`Field`: type, access,
  byte offset); its constructors, destructor, methods and static member functions — in
  `Record.Members` for a synthesised specialisation, in the project's symbols for a full explicit
  body and for non-template records (`Record.Synthesized` is the discriminator); **operators**
  (`Symbol.IsOperator`,
  `Symbol.Operator`) and **conversion operators** (`Symbol.IsConversion`, target type in
  `Symbol.Result`); **static data members** (`Record.StaticFields`: type, access, `IsConst`, and the
  definition's mangled name when libclang names the instantiation's definition — empty for a
  specialisation's static member, whose pattern mangling is not the instantiation's); **nested types**
  as records of their own (`Outer::Inner`, plus nested enums); and **member
  function templates** as patterns (`Record.MemberTemplates`) plus the concrete instantiations the
  manifest names (`Project.InstantiateMember`). A member function template without an instantiation
  is not bindable, and the error names the `-instantiate` entry to add.
- **Template specialisations have a precedence: full, partial, primary.** A **full explicit
  specialisation** (`template <> struct Q<int> { … }`) has its own definition cursor, so its fields,
  constructors, destructor, methods (with their manglings), static data, nested records and enums,
  bases and layout are read from **its own body**; it is never synthesised from the primary. A
  **partial specialisation** (`template <typename T> struct Q<T *> { … }`) is a pattern with its own
  cursor: an instantiation selects the best match by arity, then by the number of argument components
  that are not a bare parameter (more specific wins), ties in declaration order, and synthesises from
  that partial with both its parameters and the remaining ones substituted. An instantiation with no
  matching partial synthesises from the **primary**. Only these two cases synthesise: libclang emits
  no definition cursor for an implicit instantiation. `Record.LayoutKnown` says which case applies:
  **true** → the Go type is the mirrored struct (fields at their offsets, compile-time assertions);
  **false** → the same type is opaque with field accessors. Either way the record's fields, members,
  operators, static data and nested types are in the model; a member template is present as its
  pattern until an instantiation is named. A specialisation reached only through a field's type is
  built from that type, so `BoxHolder` makes `Box<double>` part of the API.
- **`New<Type>()` and `Clone()` are promised only when the model can classify the record.**
  `Record.IsDefaultConstructible` is true for a non-deleted zero-argument constructor, or no
  user-declared constructor and every field trivially constructible: a scalar, pointer, enum, a
  fixed-size array of those, or a record the model classified as trivially copyable and POD or
  default-constructible. `IsCopyConstructible` is true for a non-deleted copy constructor, or no
  user-declared copy/move constructor and every field trivially copyable by the same rule. The
  field record's own layout need not be known: a record the model classified as trivially copyable
  and POD or default-constructible qualifies even when `LayoutKnown` is false (a deferred
  specialisation), and the generated shim's compile is the final check. `Fixed<double,4>` (an array
  of doubles) qualifies; an array of a non-trivial record does not. When they are false no
  `New`/`Clone` is generated.
- **Class → Go struct with methods.** Methods call the thunk that performs real C++ dispatch, so a
  virtual method on a `*Shape` runs the most-derived override.
- **Ownership and views.** An owning value carries the destructor of the type it was **constructed
  as** and a Go-side reference count. `As<Base>()` returns a view of the same object and adds a
  reference; `Close` drops one, and the destructor runs when the last reference goes. A view is as
  valid as the value it came from, and destruction is correct even when a base pointer aliases a
  derived object and even when the destructor is not virtual. A second `Close` on the same Go value
  returns `cppgo.ErrClosed`; it never decrements twice.
- **Base classes and `this` adjustment.** For each bound class, methods declared in its public bases
  are generated on the derived Go type as well. `As<Base>()` returns a `*Base` whose address the
  thunk adjusted with `static_cast`; `As<Derived>() (*Derived, bool)` performs a `dynamic_cast` and
  reports failure instead of panicking. Multiple inheritance and virtual bases need no layout
  arithmetic in Go: the cast is written in C++.
- **Constructor → `New<Type>(...) (*Type, error)`. Destructor → `Close() error`.**
- **Free function → package function. Static member → package function prefixed with its type**
  (`VectorDims`).
- **Enum → named Go integer type plus typed constants** (`type Color uint8`,
  `const ColorRed Color = 0`), for scoped and unscoped enums alike.
- **`cppgo.Handle` appears in no exported signature.**
- **Zero values are invalid**: a method on a nil pointer or a closed value returns
  `cppgo.ErrClosed` (or `*T` methods that cannot return an error panic with a message naming the
  method, which is a programmer error). No finalizer is generated.

### Virtual dispatch

```go
// Shape is mathx::Shape. Area is virtual: the thunk calls shape->area() and the
// vtable picks the override.
type Shape struct{ obj *cppgo.Object }

func (s *Shape) Area() (float64, error)
func (s *Shape) Close() error

type Circle struct{ obj *cppgo.Object }

func NewCircle(r float64) (*Circle, error)
func (c *Circle) Area() (float64, error)     // the same thunk body, on the derived type
func (c *Circle) Radius() (float64, error)
func (c *Circle) Close() error

// AsShape returns the same object seen as its base: the thunk adjusts the
// pointer for a non-zero base offset, and the view shares ownership so the
// object stays alive until both values are closed.
func (c *Circle) AsShape() *Shape

// AsCircle is a dynamic_cast: it reports whether the object really is a Circle.
func (s *Shape) AsCircle() (*Circle, bool)
```

### `std::` types, ownership and lifetimes

```go
// describe: std::string parameters are copied in; string_view borrows the Go
// bytes for the call; the result is copied out before the thunk returns.
func Describe(prefix, suffix string) (string, error)

// doubled: a std::vector input is copied into a C++ vector; the result is
// copied back into a fresh Go slice, so no C++ object escapes.
func Doubled(values []float64) ([]float64, error)

// sum: a std::span parameter borrows the Go backing array, no copy.
func Sum(values []float64) (float64, error)

// dot3: std::array<T, N> becomes [N]T, by value.
func Dot3(a, b [3]float64) (float64, error)

// maybe_norm: std::optional<T> is *T. nil is nullopt; a non-nil pointer is the
// value. The pointer points at Go storage the binding allocated.
func MaybeNorm(v *Vector) (*float64, error)

// make_vector: the unique_ptr is released into a Go-owned handle.
func MakeVector(x, y float64) (*Vector, error)

// share: the shared_ptr reference is copied into another Go-owned handle.
func Share(v *Vector) (*Vector, error)

// BoxInt is mathx::Box<int>, instantiated by the manifest. When libclang can
// complete the instantiation it is this mirrored struct and the field offsets
// are asserted; when it cannot, the same value is opaque (obj *cppgo.Object)
// with Value()/SetValue() accessors. The members are generated either way.
type BoxInt struct {
	Value int32
}
func (b *BoxInt) Get() (int32, error)
func (b *BoxInt) Int() (int32, error)     // operator int, named after its target
func (b *BoxInt) ConvertFloat64(input float64) (float64, error)
                                          // convert<double>, from -instantiate
func BoxIntShared() (int32, error)        // static data: shared
func BoxIntSetShared(value int32)         // ...and its setter, shared is not const
type BoxIntInner struct {                 // nested type; same LayoutKnown rule
	Inner int32
}
func NewBoxInt() (*BoxInt, error)         // when the record is default-constructible
func (b *BoxInt) Clone() (*BoxInt, error) // when it is copy-constructible

// Flags is mathx::Flags. Bitfields cannot be Go fields, so the record is opaque
// and every field is an accessor. A default-constructible record gets NewFlags.
type Flags struct{ obj *cppgo.Object }
func NewFlags() (*Flags, error)
func (f *Flags) Close() error
func (f *Flags) Low() uint32
func (f *Flags) SetLow(value uint32)
func (f *Flags) High() uint32
func (f *Flags) SetHigh(value uint32)
func (f *Flags) Ordinary() int32
func (f *Flags) SetOrdinary(value int32)
```

The complete rules:

| C++ | Go | copy / borrow / lifetime |
|---|---|---|
| `std::string` parameter | `string` | **copied** into a `std::string` for the call |
| `std::string` result | `string` | **copied** out; the C++ string is freed before the binding returns |
| `std::string &` (non-const) | input `string`, result `string` | copied in and back out; the Go value is immutable |
| `std::string_view` parameter | `string` | **borrowed** for the call only; not NUL-terminated, the view carries the length |
| `std::string_view` result | `string` | **copied immediately**; a view of a dead object is never held |
| `std::vector<T>` parameter | `[]T` | **copied** into a C++ vector |
| `std::vector<T>` result | `[]T` | **copied** into a fresh Go slice, then freed |
| `std::vector<T> &` (non-const) | input `[]T`, extra `[]T` result | copied in and out; the Go slice is not aliased |
| `std::span<T>` parameter | `[]T` | **borrowed**: points at the Go backing array, valid for the call, the callee must not retain it |
| `std::span<T>` result | `[]T` | **copied immediately** |
| `std::array<T,N>` parameter / result | `[N]T` | copied |
| `std::optional<T>` | `*T` | nil is `nullopt`; the pointer is Go storage |
| `std::unique_ptr<T>` parameter | `*T` | **consumes** the Go value: passing it transfers ownership, and the Go value is invalid afterwards |
| `std::unique_ptr<T>` result | `*T` | owning: `Close()` destroys it |
| `std::shared_ptr<T>` parameter | `*T` | **shares**: the reference count is copied, the Go value stays valid |
| `std::shared_ptr<T>` result | `*T` | owning: `Close()` drops one reference |
| `T *`, `T &` parameter | `*T` | **borrowed** for the call; the callee must not retain it; `const` is honoured by the C++ signature |
| `(T *p, size_t n)` parameter | `[]T` | **borrowed**; the layout of T is checked, so the Go array is the C array; `const` decides whether the callee may write |

A non-const `std::vector<T> &` parameter is an in/out slice: the generated function takes `[]T` and
puts the resulting `[]T` in its result list, because a Go slice is a value and a mutation of a copy
would otherwise be invisible.

A `std::` type outside the table is still supported: it crosses as an **opaque value** (by pointer,
or as an owning value with `Close`), and the generated thunk constructs, copies and destroys it. Its
own members are not bound, because binding the standard library is a different job; bind a C++
wrapper when a Go caller has to call one. The single exclusion is a callback: `std::function` and
function pointers are a C++→Go call direction, a separate feature, not a value that crosses.

## Type map

Sizes are validated against libclang's `sizeof` for the translation unit; a platform where `long` is
not 64-bit is a generation error, not a silent reinterpretation.

| C++ | Go | notes |
|---|---|---|
| `bool` | `bool` | |
| `char`, `signed char` | `int8` | plain `char` is signed on the supported targets |
| `unsigned char` | `uint8` | |
| `short`, `unsigned short` | `int16`, `uint16` | |
| `int`, `unsigned int` | `int32`, `uint32` | |
| `long`, `long long`, `unsigned long`, `unsigned long long` | `int64`, `uint64` | LP64, validated |
| `int8_t`…`uint64_t` | the same width | fixed-width aliases resolve by size |
| `intptr_t`, `ptrdiff_t` | `int64` | validated |
| `size_t`, `uintptr_t` | `uint64` | the length half of a slice pair is not visible in Go |
| `float`, `double` | `float32`, `float64` | |
| `wchar_t`, `char16_t`, `char32_t` | `int32`, `uint16`, `uint32` | validated by size |
| enum (scoped or unscoped) | named integer type + constants | underlying type from libclang |
| `T *`, `T &` | `*T` | borrowed; `[]T` for the pair form |
| `(T *p, size_t n)` | `[]T` | borrowed; `const` in the C++ signature decides whether the callee may write through it |
| `(char *p, size_t n)` / `(const char *p, size_t n)` | `string` | borrowed for the call; not NUL-terminated |
| mirrorable record, by value | `T` | the Go struct *is* the C layout |
| non-POD record, by value | `*T` | the thunk copies into the call; `T` owns Go-pinned storage |
| mirrorable record, `T &` | `*T` | writes are visible in the Go value |
| `T[N]` (record field) | `[N]T` | fixed-size array; a dependent bound is resolved from the template argument |
| `std::` types | as in the table above | always through a thunk |

A C array parameter decays to a pointer at the C++ boundary and maps by the pointer rule; only
record fields are arrays. A field of array type in a mirrored record is `[N]T`; in an opaque record
the accessor returns `[N]T` and the setter takes one, copied element by element. A dependent `T[N]`
in a specialisation is resolved when the arguments are substituted, so `Fixed<double,4>::values` is
a `[4]float64` field (or its accessor).

A pointer parameter followed by a length parameter of integer type becomes a slice: the pointer is
the data, the length is `len(slice)`, and the slice is borrowed for the call. **One length may serve
several pointers**: in `saxpy(double, const double *, double *, size_t)` both pointers become `[]T`
and share the one `n`, which is the BLAS shape. The generated binding checks before the call that
every slice sharing a length has the same length, and returns an error when they do not; the C++
side still receives exactly one length. A length parameter that precedes no pointer is an ordinary
`uint64` parameter.

`long double` and `__int128` have no Go value type; they are the one family of scalar the type map
cannot carry. A signature that uses one is a generation error with the fix "widen/narrow it at the
C++ boundary to `double`/`int64` or wrap it in a supported type". This is an unrepresentable *input
type*, not a refusal of a callable construct: any C++ function that takes or returns one can be
reached through a one-line C++ wrapper with a representable signature.

## Naming and overloads

- A name is CamelCase, split on `_`, leading and trailing underscores stripped: `count_above` →
  `CountAbove`, `x_` → `X`, `saxpy` → `Saxpy`.
- A class or struct is its own name; a namespace contributes nothing. Two declarations from different
  namespaces that map to one Go identifier are resolved with `-rename` (below); without one it is a
  generation error naming both.
- `New<Type>` for a constructor, `Close` for the destructor, `<Type><Name>` for a static member:
  `mathx::Vector::dims` → `VectorDims`.
- **Constructor overloads** follow the same suffix rule as functions, applied to `New<Type>`:
  a class with one constructor gets `NewVector`; a class with several gives each of them the
  parameter-type suffix, with the zero-parameter constructor keeping the bare name —
  `Vector()` → `NewVector`, `Vector(double)` → `NewVectorFloat64`,
  `Vector(double, double)` → `NewVectorFloat64Float64`. A copy constructor is `Clone()`, not a
  factory, and a move constructor is not exposed separately (Go has no rvalue overload). Two
  constructors whose Go parameter types are equal need `-rename '<qualified ctor
  signature>=<GoName>'`.
- A name with **one** declaration keeps its bare CamelCase form. A name with **several** overloads
  takes a suffix built from its Go parameter types, in order: `dot()` → `Dot`,
  `dot(const double *, size_t)` → `DotFloat64Slice`, `dot(Vector const &)` → `DotVector`. The
  zero-parameter overload keeps the bare name.
- A **class template specialisation** is `<TemplateName><Arguments>`: `mathx::Box<int>` → `BoxInt`,
  `mathx::Box<mathx::Vector>` → `BoxVector`, `mathx::Pair<int, double>` → `PairIntFloat64`. A
  **function template** binding is named the same way from its specialization.
- `Open` is reserved as the package-level entry point, and `Close` as the destructor method of every
  class. A non-destructor C++ member that maps to `Close` collides with the destructor and needs
  `-rename`; a free function mapping to `Open` collides with the package entry point and needs one
  too. A destructor is never a collision: it *is* `Close`.
- A **nested type** is the outer Go name followed by its own: `mathx::Operators::Entry` →
  `OperatorsEntry`, `mathx::Box<int>::Inner` → `BoxIntInner`; deeper nesting concatenates in order.
  A nested **enum** is a type too: `mathx::Box<int>::Kind` → `BoxIntKind`, with constants
  `<EnumName><Enumerator>` (`BoxIntKindA`, `BoxIntKindB`). Its underlying type and initializers come
  from the body that defines the instantiation: a full explicit specialisation's own enumerators, a
  matched partial's, or the primary's. When an initializer is neither a literal nor a template
  parameter the model reports `Enum.ConstantsKnown == false` and the generated enum carries no
  constants; a constant is never taken from a body the instantiation does not use. A collision needs
  `-rename`.
- A **conversion operator** is named after its target Go type: `operator int` → `Int()`,
  `operator double` → `Float64()`, `operator std::string` → `String()`, `operator bool` → `Bool()`;
  a record or pointer target is `As<RecordName>` (`operator mathx::Vector` → `AsVector`). A collision
  with another member needs `-rename`.
- **Operators take a fixed name.** The table below is the whole rule; anything not in it, or a
  collision, is a generation error asking for `-rename '<qualified signature>=<GoName>'`:

  | C++ | Go | C++ | Go |
  |---|---|---|---|
  | `operator+` | `Add` | `operator==` | `Equal` |
  | `operator-` (binary) | `Sub` | `operator!=` | `NotEqual` |
  | `operator-` (unary) | `Neg` | `operator<` | `Less` |
  | `operator*` | `Mul` | `operator<=` | `LessOrEqual` |
  | `operator/` | `Div` | `operator>` | `Greater` |
  | `operator%` | `Mod` | `operator>=` | `GreaterOrEqual` |
  | `operator[]` | `At` | `operator=` (copy assignment) | `Assign` |
  | `operator()` | `Call` | `operator++` / `operator--` | `Inc` / `Dec` |
  | `operator&` / `|` / `^` / `~` | `BitAnd` / `BitOr` / `BitXor` / `BitNot` | `operator&&` / `||` / `!` | `And` / `Or` / `Not` |
  | `operator+=`, `-=`, `*=`, … | `AddAssign`, `SubAssign`, `MulAssign`, … | `operator<<` / `>>` on a stream | `WriteTo` / `ReadFrom` |

  A **stream type** is `std::ostream`, `std::istream`, `std::wostream`, `std::wistream`, or a
  `std::basic_ostream<C,T>` / `std::basic_istream<C,T>` specialisation, recognised by its **declaration
  spelling** (libclang's canonical form of the `ostream` alias can resolve to `int`, so the spelling
  is the reliable half; the model exposes `Param.IsStream()`). When either operand is a stream,
  `operator<<`/`>>` are `WriteTo`/`ReadFrom`; otherwise `ShiftLeft`/`ShiftRight`.
  `operator new`/`operator delete` are absent from the table on purpose — the Go side allocates — so
  they follow the same single rule as any unlisted operator: a generation error unless `-rename`
  names the binding.
- A **static data member** is a package function `<Type><Field>()` returning the value (the getter
  copies for `std::string`/containers, per the type map; a non-mirrorable record is copied into a new
  owning Go value, and a `static T *` is borrowed by the pointer rule), and `<Type>Set<Field>(v)` only
  when it is not `const`. `mathx::Operators::counter` → `OperatorsCounter()` / `OperatorsSetCounter(v)`;
  `limit` (const) has only the getter. A static data member of a specialisation takes the
  specialisation's Go name: `BoxIntShared()`.
- A **member function template** is bound for each instantiation the manifest names
  (`-instantiate 'mathx::Box<int>::convert<double>'`) or that a bound signature reaches; the Go name
  follows the function rule on the instantiated signature (`convert<double>` → `ConvertFloat64`).
  The instantiation carries its substituted signature. libclang exposes no cursor — and so no
  mangled name — for an explicit member instantiation, so the binding is a thunk that calls the
  instantiated member directly; the Go API is the same as for any other member. Selecting the
  pattern without an instantiation is an error naming the instantiation to add.
- When the derived name collides with another binding, the manifest supplies the name:
  `-rename 'mathx::Vector::dot(const double *)'=DotSlice`. A `-rename` that matches nothing is an
  error, so a typo cannot silently do nothing.

The **binding manifest** is the command line that names the roots and the specialisations:

```
cppgo gen -header <path> -package <name> -library <name-or-path> -out <dir>
          [-bind <qualified>]... [-exclude <qualified>]... [-status <qualified>]...
          [-instantiate '<template specialization>']... [-rename '<signature>=<GoName>']...
          [-compile-commands <path>]
cppgo check ...        # the same run, writes nothing, exits 1 when a file is stale
```

## `this` adjustment, multiple inheritance and virtual bases

A generated thunk takes the pointer the Go side holds at its **exact static type**. Every conversion
between types is written in C++ and compiled where the layout is known:

- upcast: `static_cast<Base *>(derived)` — a constant offset for a non-virtual base, a vtable lookup
  for a virtual base;
- downcast: `dynamic_cast<Derived *>(base)` — `nil` when the object is not a `Derived`; the Go
  helper returns `(*Derived, bool)`;
- method calls: `object->method()` with `object` at the declared type, so the compiler emits the
  correct `this` adjustment and, for a virtual method, the correct dispatch.

The model exposes what the generator needs to choose these conversions: every record's base list
with access, virtual-base flag and offset (`-1` when the offset is only known at run time), and each
method's declaring class. Go never adds an offset by hand.

## Errors and exceptions

**Every call that can throw returns an `error` as its last result.** The rule is exact:

- a **direct** binding is `noexcept` by construction (a non-`noexcept` function goes through a thunk)
  and performs no marshalling, so its Go signature has no error. Constructors and `Close` keep theirs
  for allocation and lifetime failures;
- a **thunk** binding returns an error when the C++ call is not `noexcept`, **or** when the thunk
  itself can throw while marshalling (constructing a `std::string`, `vector`, `array`, optional or
  smart pointer, copying a non-POD, or allocating a result buffer can always raise `bad_alloc`). A
  thunk over a `noexcept` function whose arguments are scalars, pointers or PODs cannot throw, and
  its Go signature has no error:
  `func Translate(p Point, dx, dy float64) Point`;
- a **destructor** is `noexcept` unless declared otherwise; `Close() error` always exists and reports
  `cppgo.ErrClosed` for a dead receiver and any exception a `noexcept(false)` destructor raises.

How a failure arrives:

- the thunk wraps the call in `try { … } catch (const std::exception &e) { … } catch (...) { … }`;
- the message is stored in a generated thread-local string and exported as
  `extern "C" const char *cppgo_last_error()` in `zz_cppgo_shim.h`;
- a thunk that can fail returns a non-zero `int32_t` status, and the value, if any, is written
  through an out-pointer; a thunk that cannot throw returns its value directly;
- the generated Go wrapper reads the message, wraps it in `*cppgo.CallError{Symbol, Err}`, and
  returns it. The error formats as `cppgo: mathx::describe: <message>`.

The direct ABI has no handler, which is why only `noexcept` functions may use it: an exception
escaping a NOSPLIT trampoline would unwind into a Go frame that has no handler.

The **`-status` convention** remains available for C++ APIs that report failures as a 32-bit return
code: `0` means success, non-zero means the library stored a message in `cppgo_last_error()`. The Go
binding returns `error` instead of the integer and wraps the message in `*cppgo.CallError`. It is
independent of, and can be combined with, exceptions.

Typed errors: `*OpenError`, `*SymbolError`, `*NativeError` (the message itself), `*CallError` (which
wraps the native error), and the sentinel `ErrClosed`.

## The ABI the direct path calls

Itanium C++ ABI on SysV AMD64, which is what the generator validates against and the trampoline
implements:

- Integer and pointer arguments: `RDI, RSI, RDX, RCX, R8, R9` in order.
- Float arguments: `XMM0…XMM7` in order; the two files are independent.
- A hidden sret pointer, when there is one, takes the first INTEGER slot; `this` takes the next;
  the explicit arguments follow.
- Result: `RAX` for integer and pointer, `XMM0` for float, up to two registers for a 16-byte POD.
- `this` is the object's address. `const` members are ordinary calls; the `K` in the mangling is
  reported, never assembled by hand.
- Argument registers are not spilled to the stack by the generated trampoline: more than 6 INTEGER
  or 8 SSE slots, or a by-value aggregate outside the 16-byte register class, routes the binding to
  a thunk, which takes a pointer to an argument block or to the value instead. This is a carrier
  decision, not a rejection. **Results** are different: an sret result is written through the hidden
  pointer into Go storage.
- A slice or string parameter is `(pointer, length)`, pointer first, adjacent in the integer file.
- The callee may not retain a pointer into Go memory after the call returns, and the caller keeps
  the memory alive with `cppgo.KeepAlive`.

## The trampoline

Measured 2026-11-03, Go 1.27, an 8-core x86-64 CPU pinned to 964 MHz with boost disabled — the
absolute nanoseconds are inflated by roughly 4.5×, so **cycles and ratios are the honest numbers**:

| path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| **cppgo trampoline, direct** | **13.9** | **13** |
| cppgo trampoline, stack switch | 14.9 | 14 |

The direct path is ~7.5× cheaper than cgo and within ~2× of a plain Go call. A thunk adds one C++
call frame inside the library; the Go-side cost is the same trampoline. `make bench` reproduces the
numbers; the README and the site quote them with the clock caveat.

One signature, as generated:

```asm
TEXT ·callNorm(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVQ	SP, R13
	ANDQ	$~15, SP        // SysV: RSP 16-aligned *before* the CALL
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+16(FP)
	RET
```

Rules that are not negotiable:

- Arguments are read **before** SP is switched and the result is stored **after** it is restored;
  `FP` is relative to SP.
- `NOSPLIT`, zero frame, no allocation, no Go memory touched, no callback into Go.
- `ProbeStack()` runs before the trampoline, from the same goroutine and at or above the foreign
  call. Raising the stack pointer's distance from `stack.lo` is done by entering an assembly
  function with a 32 KiB frame, not a Go frame with a 32 KiB local array (the compiler zeroes it,
  ~1.2 µs measured).

Correctness evidence, all still required on every run: a NOSPLIT asm function is never
asynchronously preempted (`runtime.isAsyncSafePoint` is false for any asm frame and for any
`sp < gp.stack.lo`), GC torture with `GOGC=1` and `runtime.GC()` under 32 goroutines, SIGPROF/SIGURG
torture, `-race`, and `GOMAXPROCS=1`. `//go:linkname runtime.asmcgocall` is rejected by the linker;
do not try it. The documented trade-off stands: a foreign call in flight delays a GC cycle by up to
its duration (2.0 ms call ⇒ 1.1 ms worst cycle), because the P is not released.

## Loading: purego stays

`github.com/ebitengine/purego` is the only third-party runtime dependency, and this is deliberate.
Do not replace it.

Reimplementing `dlopen`/`dlsym` without cgo means `//go:cgo_import_dynamic` directives, per-platform
assembly stubs, and a dynamic loader that breaks quietly when a libc changes symbol versioning. The
decisive reason is not familiarity: a dynamically linked Go binary does not have a libc thread
control block, so any libc code reading thread-local storage — `errno`, `dlerror`,
`pthread_getspecific` — runs against a TCB glibc never initialised. purego solves that in
`internal/fakecgo`, which its non-cgo path imports on purpose. Writing our own loader would mean
owning fakecgo too, which is larger and less forgiving than the loader.

We use only `purego.Dlopen`/`Dlsym` — never purego's call path. The long tail is the generated
shim, not somebody else's calling convention: everything the trampoline cannot call becomes C++ in
`zz_cppgo_shim.cpp`, so there is exactly one convention to reason about. Our differentiator is not
the loader: it is knowing at generation time what every binding compiles to at all.

## Generation: input, selection, evidence, output

- **`-header`** is the C++ header to bind; it may be repeated. **`-package`** is the Go package name.
  **`-library`** is the shared object: the name `Open("")` loads, and, when it is a readable file,
  the ELF symbol table every **direct** binding is checked against.
- **Selection.** With no `-bind`, every public declaration in the headers is a root. Every type that
  appears in a bound signature is generated too (the type closure), even when it is not a root.
  `-bind` adds a root (a namespace, a class or struct, a member, or a function); `-exclude` removes
  one. They name declarations; they never describe a signature. An unmatched `-bind` or `-exclude`
  is an error, so a typo cannot silently shrink the API.
- **Compilation flags** come from `compile_commands.json` and `.clangd`, in clangd's order, never
  invented: `-compile-commands` explicitly, else the nearest database walking up from the header
  (honouring `CompileFlags.CompilationDatabase`), else `.clangd`'s `CompileFlags.Add/Remove/Compiler`.
  The fallback `-std=c++17 -I<header dir>` exists but the report says it was assumed.
- **Evidence.** libclang answers types, sizes, offsets, visibility, inline-vs-out-of-line, virtual,
  exception specification, defaulted/deleted special members, template arguments and base lists.
  The ELF tier is a **cross-check** for direct bindings: `internal/cxx.DefinedDynamicSymbols` reads
  the `.dynsym` of `-library` and the generator fails when a direct binding's mangled name is not
  exported. Thunk symbols exist only after the user compiles the generated shim; the shim itself is
  parsed by libclang before it is written, and `Open` resolves every thunk at load time.
- **Output.** `zz_cppgo.gen.go` (the API), `zz_cppgo_amd64.s` (the trampolines), `zz_cppgo_shim.h`
  and `zz_cppgo_shim.cpp` (the thunks). The shim is compiled by the user's normal C++ build. The
  generated Go file carries the exact invocation that produced it, so the regeneration command lives
  in the repository.
- **Determinism.** The same inputs produce byte-identical output regardless of file-system order or
  of how many headers were parsed; bindings are sorted by qualified name and thunk names come from a
  stable hash of the qualified signature. `cppgo check` is the CI gate, and the golden files under
  `internal/gen/testdata` are its unit-level equivalent.
- **Comments.** The C++ doc comment, when there is one, becomes the Go doc comment. Every binding
  then carries exactly one evidence line —
  `// C++: mathx::Vector::norm() const, _ZNK5mathx6Vector4normEv` — and, when the ELF cross-check
  ran, it ends `, verified against libmathx.so`. Nothing else is generated into a comment: the ABI
  register layout belongs in this file, not repeated on every wrapper.

**Validation is coverage, not permission.** For every binding the generator proves that a carrier
exists and that the code generated for it compiles: a direct symbol is cross-checked against the
library's dynamic symbols, a thunk is parsed through libclang with the same flags. Nothing is judged
acceptable or unacceptable; the only errors left are the inputs below, which name no Go construct and
come with what the user supplies.

## What remains a generation error

Only **input that names no Go construct**, each with the thing the user supplies to make it
representable. None of these is "this construct cannot be bound":

| input | the error names | what the user supplies |
|---|---|---|
| a template selected as a root (or reached) with no concrete instantiation | the template and the parameter that is unknown | `-instantiate 'mathx::Box<int>'` |
| a member function template selected (or reached) with no instantiation | the member template and its parameters | `-instantiate 'mathx::Box<int>::convert<double>'` |
| two bindings that map to one Go name and no suffix separates them | both declarations | `-rename '<qualified signature>=<GoName>'` |
| `long double` / `__int128` in a signature | the parameter or result | a C++ wrapper with a representable type |
| an incomplete type used where a value is needed | the type and the use | include the definition |

There is no flag that emits an unvalidated binding. Every binding has a carrier, every thunk is
parsed before it is written, and every symbol is resolved at `Open`.

## Not part of this design

- the Go directive DSL (`//cppgo:library`, `//cppgo:header`, `//cppgo:class`, `//cppgo:ctor`,
  `//cppgo:dtor`, `//cppgo:method`, `//cppgo:function`, `//cppgo:external`, `//cppgo:abi`) and the
  stub file it was rewritten into; the repo's bindings come from C++ only;
- a **hand-written** `extern "C"` shim layer. The shim is now generated infrastructure and is
  mandatory wherever the direct ABI does not reach; there is no user-authored shim mode;
- the hand-written ABI annotation tier and any flag that emits a binding libclang did not
  understand;
- `cppgo.Handle` in a generated API.

Non-goals: callbacks from C++ into Go (a reverse-direction feature, not a type mapping, and outside
the construct list this contract covers); arm64 and Windows code generation (the Go package still
builds; the generator does not emit trampolines for an unsupported target and the error names it).

## Tests and coverage

- `make test` runs two passes, both required:
  `CGO_ENABLED=0 go test -count=1 ./...` (the no-cgo contract) and
  `CGO_ENABLED=1 go test -race -count=1 ./...` (the race detector; the toolchain rejects `-race`
  with cgo off). Coverage is collected on the cgo-enabled pass with `-covermode=atomic`.
- Table-driven tests with `t.Run` per case, `testify` `require`/`assert`, `t.Parallel()` where cases
  are independent. Concurrency tests run under `-race` and `GOMAXPROCS=1` with at least 32
  goroutines.
- Coverage gate: **≥ 99%** on `./...`, measured by `./scripts/coverage-gate.sh`. The gate's
  exclusions are exactly the three it prints, each with its reason:
  `zz_cppgo*.go` (generator output), `internal/gen/testdata/` (fixtures the generator writes and
  diffs), and `internal/testlib/` (test scaffolding: it compiles the C++ fixture library other tests
  load and has no tests of its own). **No hand-written production package is excluded**, and `cmd/`
  is measured like everything else — which is why `run(args, stdout, stderr) int` carries all the
  CLI logic and `main` is one statement. The contract and the script must name the same list; a
  fourth exclusion is a contract change, not a local edit.
- **Carrier coverage is a test**: every binding in the fixture set is asserted to have a carrier,
  and the golden output is compiled and run for both carriers.
- **The generated shim is compiled by the test suite** (`g++`/`clang++` with the fixture's flags),
  so "generation-time validation" is measured against a real compiler, not against a model.
- Every error path has a test: missing library, missing thunk, missing symbol, empty string, closed
  library, an exception translated into a message, a status function whose message is empty.
- The runtime has a test that fails if `internal/cxx` appears in the root package's dependency graph,
  so the two layers cannot quietly merge.
