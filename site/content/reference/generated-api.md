---
title: "Generated API"
description: "The Go shapes cppgo gen writes for records, classes, functions and templates, and the C++ declarations they come from."
weight: 20
---

This page describes the package `cppgo gen` writes. `cppgo gen` reads a C++
**header** — the `.hpp` file that declares a library's classes and functions —
and writes a Go **binding**: one Go identifier per C++ declaration, plus the code
that loads the library and calls it. Every shape is shown with the C++ declaration
it comes from — copied from the header in the repository, linked — and the Go it
became, from a generated package in the examples. The C++ is the input; the Go is
the output. Where a generated function is shown without its body, the body is in
the linked file and adds nothing a reader needs to recognise the shape.

## Package entry

Every generated package has one way in. These two are not generated from a C++
declaration: `Open` loads the two **shared objects** — the `.so` files the
dynamic loader, the part of the operating system that maps libraries into a
process, opens at run time — and `CppgoLibraryPath` says where the user's library
is. Of the two objects, the first is the **shim**, the C++ that `cppgo gen`
generates and compiles, and the second is the library the user built, which the
shim links against.

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
no explicit call. `""` means the shim name the generator recorded. A missing
symbol is reported once, as
`the shim cppgo compiled is stale; run cppgo gen again`: the shim on disk was
built from a different header than the Go package.

## Records

A C++ **record** is a `struct` or a `class`. A record whose fields are all
public and whose layout matches Go's becomes a Go struct with the same fields. A
record with no constructor, no virtual function and no base class is called
**POD**, plain old data: its bytes are its value, so both languages can pass it
by value.

### A record with a natural layout becomes a Go struct

This is `mathx::Point`, in
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

A `Point` is passed and returned by value: the Go struct has the C++ layout, and
the generated code proves that at compile time. `NewPoint` returns the zero
value, and `Clone` returns a copy. The C++ field names become exported Go field
names: `x` is `X` and `y` is `Y`.

A record that does not qualify becomes an **opaque** value instead: Go holds
only a pointer to storage whose layout it cannot see, and the generated methods
and `Close` are its whole surface.

### A class becomes a struct with a lifetime

A **class** is a record with member functions and access control — the
`public` and `private` labels that say who may use a member. A
**constructor** builds an object; a **destructor** destroys it; a non-static
member function receives `this`, a hidden pointer to the object it was called on;
and a **static member** belongs to the class rather than to an object, so it has
no `this`. A **copy constructor** is the constructor that takes another object of
the same class and copies it. The generated Go value has a **lifetime**: it owns
C++ storage, and the Go program destroys that storage with `Close`.

The class below also uses `std::size_t`, the C++ standard library's unsigned
integer wide enough to hold a size — Go's `uint64` on this platform.

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

`obj` is a `*cppgo.Object` ([Runtime]({{< ref "reference/runtime.md" >}})#objects):
Go-allocated storage pinned so its address stays valid while C++ holds it.

The rules that hold for every class:

- `New<Type>` is a constructor. A class with more than one constructor has an
  **overload set** — two declarations of the same name that differ in parameter
  types — and a parameterless constructor keeps `New<Type>` while every other
  one appends the CamelCase Go type of each parameter, as functions do.
- `Close` releases the object. There is no finalizer — no function the garbage
  collector runs to destroy it — so closing is the caller's job.
- `Clone` is the copy constructor. A move constructor — the C++ constructor that
  takes over another object's storage instead of copying it — is not exposed.
- A static member with no `this` becomes a package function named after its type:
  `func VectorDims() (int32, error)` is `mathx::Vector::dims()`. Go has no class
  scope, so the type name is the prefix.
- Accessors keep their names: `X`, `Y`, `Norm`, `Dot`, `At`, `Validate`,
  `Scale`.

### Zero and closed values

A method on a nil or closed value returns `cppgo.ErrClosed` — the error a closed
value reports. The C++ side is the inline accessor and the `noexcept` setter in
the class above. `noexcept` is C++'s promise that a function will not throw a
**C++ exception** — the language's error-signalling mechanism, which unwinds the
stack until a handler catches it:

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

Line by line:

- `v == nil || v.obj == nil` is the one failure that never reaches C++: a nil or
  closed receiver has no object to call, so the method returns `ErrClosed`.
- `cppgoOpen("")` loads the shim on first use; `""` is the name the package was
  generated with, as in `Open`.
- `cppgo.ProbeStack()` makes the stack room the C++ frame needs, before any
  address into the frame is taken.
- `cppgoCall_00064a4bc112(cppgoSym_00064a4bc112, v.cppgoAddr(), unsafe.Pointer(&cppgoResult))`
  is the generated assembly **trampoline** for this binding — a small assembly
  function that loads the arguments into the C registers and calls the resolved
  address. `cppgoSym_00064a4bc112` is the address `Open` resolved for the symbol,
  `v.cppgoAddr()` is the receiver's C++ address, and the third argument is the
  address of the result — `unsafe.Pointer`, Go's untyped pointer, is how an
  address into Go memory is handed across.
- `cppgo.KeepAlive(v, &cppgoResult)` keeps the receiver and the result reachable
  while C++ reads and writes them.
- the returned status is `0` for success and non-zero for failure;
  `cppgoCallError` builds a `*cppgo.CallError` carrying the message the shim
  stored.

A method whose declaration cannot fail has no error result to return, so it panics
instead. This is the generated `Scale`, unchanged:

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
fail, so its Go signature has nowhere to put an error. The only failure left is
the Go-side one, a dead receiver, and a function with no error result reports it
by panicking.

## Functions and overloads

A C++ **free function** — a function that belongs to no class — becomes a Go
package function. These are two of the functions of the `metrics` library in
[`examples/full/include/metrics/metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp):

```cpp
Point centroid(Point a, Point b) noexcept;

std::string describe(const std::string &prefix, const Point &p);
```

and these are the Go functions they became, from
[`examples/full/gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
func Centroid(a Point, b Point) Point
func Describe(prefix string, p *Point) (string, error)
```

`centroid` is `noexcept` and returns a POD, so its Go signature has no error
result. `describe` is not `noexcept` — it builds a `std::string`, the
standard-library byte-string type, which can throw — so it returns
`(string, error)`. Go has no reference type, so the C++ reference
`const Point &p` becomes the pointer `p *Point`; the caller passes `&p`.

An **overload** is two declarations with the same name in the same scope that
differ in parameter types; C++ chooses one by the arguments. The generator writes
a distinct Go name for each: a declaration with no parameters keeps the bare name,
and every other one appends the CamelCase Go type of each parameter, in order. A
pair the suffix cannot separate, or a name that collides with another Go
identifier, is a generation error naming both declarations, and
`-rename '<signature>=<GoName>'` is the way out
([Command line]({{< ref "reference/cli.md" >}})).

A **default argument** — a parameter the C++ declaration gives a value, so a C++
caller may omit it — becomes one binding per allowed prefix of the parameter
list: the full signature keeps the bare Go name, and a shorter prefix takes the
overload suffix. The default is applied by the shim's compiler, not by Go. A
prefix with no parameter at all is dropped with a warning, because it would take
the full signature's name.

## Values: the type map

The generator maps each C++ type to a Go type. These declarations of the
`metrics` and `stats` examples use the mapping:

```cpp
std::vector<double> scaled(const std::vector<double> &values, double factor);

void histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo,
               double hi);

const double *data_pointer(const double *xs, size_t n) noexcept;
```

and these are the generated signatures, from
[`examples/full/gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go)
and
[`examples/stats/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/stats/zz_cppgo.gen.go):

```go
func Scaled(values []float64, factor float64) ([]float64, error)
func Histogram(xs []float64, bins []float64, lo float64, hi float64) error
func DataPointer(xs []float64) *float64
```

The standard-library types in the table: `std::string` is a byte string,
`std::string_view` a borrowed view of one, `std::vector<T>` a growable array,
`std::span<T>` a borrowed view of a range, `std::array<T,N>` a fixed-size array,
and `std::optional<T>` a value that may be absent, with `nullopt` as the absent
value. `std::size_t` is an unsigned integer wide enough for a size,
`std::ptrdiff_t` its signed counterpart, and `std::int8_t` … `std::uint64_t` the
fixed-width integers.

Neither `scaled` nor `histogram` is `noexcept`, so both Go signatures return an
error; `data_pointer` is `noexcept` and returns the pointer alone.

A pointer and the count that follows it spend the pointer's position and become
one slice: `(const double *xs, std::size_t n)` is `xs []float64`, and a mutable
`double *bins` with its `nbins` is `bins []float64`, borrowing the caller's
array so the writes reach it. A lone pointer with no count stays a pointer:
`const double *` becomes `*float64`. A `std::vector` is copied in and out, because
the Go slice and the C++ vector do not share storage.

| C++ | Go | Notes |
|---|---|---|
| `bool` | `bool` | |
| `char`, `signed char`, `unsigned char` | `int8`, `uint8` | |
| `short`, `int`, `long`, `long long` and unsigned | `int16`/`int32`/`int64`, unsigned alike | LP64, the Linux convention: `int` is 32 bits and `long` 64 |
| `std::int8_t` … `std::uint64_t` | the same width | |
| `std::size_t`, `std::ptrdiff_t` | `uint64`, `int64` | |
| `float`, `double` | `float32`, `float64` | |
| `T *`, `T &` | `*T` | borrowed; a lone pointer or reference, with no paired count |
| `const char *` | `string` | copied in and out, NUL-terminated |
| `(const T *p, std::size_t n)` | `[]T` | borrowed for the call, nothing copied. `T` other than `char`; `n` is the count that follows the pointer |
| `(const char *p, std::size_t n)` | `[]int8` | borrowed; the pointer-and-length pair rule wins over the `string` mapping above |
| a `T *` followed by its count | `[]T` | borrowed, and writes in the callee reach the caller's slice |
| `std::string` | `string` | copied in and out |
| `std::string_view` | `string` | borrowed for the call |
| `std::vector<T>` | `[]T` | copied in and out; the mapping is applied again to `T`, so `std::vector<std::vector<T>>` is `[][]T` and `std::vector<Opaque>` is `[]*Opaque` |
| `std::span<T>` | `[]T` | borrowed |
| `std::array<T,N>` | `[N]T` | by value |
| `std::optional<T>` as a parameter | `*T` | nil is `nullopt` |
| `std::optional<T>` as a result | `*T` | nil means absent, and absence is not an error |
| `std::shared_ptr<T>` / `std::unique_ptr<T>` as a result | `*T` | the object the pointer owns; `Close` releases it. A smart-pointer *parameter* is not bound |
| `std::span<T>` as a result | — | not bound: a view of storage Go does not own |
| `T[N]` as a record field | `[N]T` | fixed-size array |
| function pointer, `std::function<R(Args…)>` | `func(…) R` | see *Callbacks* |

Some shapes are not bound rather than guessed — `long double`, `__int128`,
`nullptr_t`, `void *`, an owning smart pointer as a *parameter*, a `std::span`
as a *result*, a `non-const` reference to a standard-library value, and a
standard-library type outside the table. Each is named in a warning with a
workaround. The
[limits page]({{< ref "explanation/limits.md" >}}) has the messages.

**A non-`const` `char *` parameter does not bind.** The callee would write into
an immutable Go `string`, so the warning asks for `const char *`, which crosses
as a `string` in both directions. Use `std::string` or `std::string_view` when
the C++ side needs one object to own or view the bytes.

## Callbacks

A **callback** is a Go function that C++ calls. A parameter that takes a function
pointer — a C++ value holding the address of a function, such as
`double (*fn)(double)` — or a `std::function` — the standard-library type that
stores any callable with a given signature — becomes a Go function value. This is
`metrics::apply`, from
[`examples/full/include/metrics/metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp):

```cpp
double apply(double (*fn)(double), double value);
```

and this is the Go function it became, from
[`examples/full/gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
func Apply(fn func(float64) float64, value float64) (float64, error)
```

A function pointer a typedef names — `using LogCallback = void (*)(int, const
char *);` followed by a parameter of type `LogCallback` — is the same thing: the
alias is read as the pointer it names.

The call enters C++ through the **reentrant** carrier — the call path that marks
the thread as being inside a foreign call, a call from Go into code that was not
compiled by Go, which is the only state from which C++ may call back into Go. A
binding that passes no Go function, in a package that registers no retained
callback, does not need it.

### What a callback receives

The callback reads the C argument registers, so its arguments are the values the
C++ side put there. The bytes behind a pointer are the C++ side's: the callback
may read them, and Go must copy anything it keeps.

| C++ argument | Go | Lifetime |
|---|---|---|
| an integer, `bool`, `double`, an enum | the same Go type | a value |
| `const char *`, and `const char *` + `std::size_t` | `string` | copied while the callback runs: the Go string owns its bytes |
| `const T *` + `std::size_t` | `[]T` | a view of the C++ side's memory, valid only while the callback runs |
| `T *` (a scalar, a mirrored record) | `*T` | the same: a view of the C++ side's memory |
| `void *` | `unsafe.Pointer` | the same, and nothing says what it points at |

`(const std::uint8_t *, std::size_t)` is therefore `[]uint8`: a byte buffer whose
memory belongs to the caller of the callback, which is the reason it can be
handed to Go at all. Keeping that slice past the callback's return keeps a
pointer into memory the library may reuse; copy it with `append([]byte(nil), b...)`
if it has to outlive the callback.

The limits are the register file and the direction of the exception:

- the callback takes at most **six integer and eight floating-point arguments**,
  and one result. A callback that needs more is left out with a warning naming
  the argument;
- a `float32` argument is not bound (take a `double`), and a record, a
  `std::string` or an array *by value* is left out with a warning: there is no
  register shape for one;
- a panic inside the callback does not reach C++: the result is zeroed and the
  panic is re-raised in Go when the foreign call returns;
- a callback that has to report a failure writes a status and the C++ side checks
  it. A C++ exception that unwinds through a Go frame terminates the process, so
  catch it on the C++ side before the callback returns.

### Retained callbacks

A function pointer says nothing about how long the callee keeps it. A parameter
that C++ **stores** and calls again, during later calls of its own, is declared
with `-retain`: the name of the declaration whose callback the callee keeps.

This is the logging library of
[`internal/gen/testdata/retained/include/retained/retained.hpp`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/retained/include/retained/retained.hpp),
where the setter stores the pointer and `log` calls it:

```cpp
using LogCallback = void (*)(std::int32_t level, const char *message);

void set_log_callback(LogCallback fn) noexcept;
void log(std::int32_t n) noexcept;
```

```console
$ cppgo gen -header include/retained/retained.hpp -package retained \
    -library build/libretained.so -out ./gen \
    -retain 'retained::set_log_callback'
```

The registering call returns a **registration**, and the function stays
registered until it is closed:

```go
func SetLogCallback(fn func(int32, string)) (*CallbackRegistration, error)

func Log(n int32)

// Close releases the registration, and reports cppgo.ErrClosed when it already
// was released.
func (r *CallbackRegistration) Close() error
```

```go
// Registered once; the call returns and the records keep arriving.
logs, err := retained.SetLogCallback(func(level int32, message string) {
	fmt.Println(level, message)
})
if err != nil {
	return err
}
defer logs.Close()

retained.Log(3) // three calls into the callback, during this call from Go
```

The rules a registration lives by:

- **any call may deliver.** A retained callback can be invoked from inside any
  call into the library, so every binding in a package generated with `-retain`
  enters C++ through the reentrant carrier. The report names that reason on every
  line;
- **a C++ thread Go did not start must not call it.** The callback runs on the
  goroutine that made the call, and only while C++ is inside a call made from Go.
  A C++ background thread that calls it reaches the runtime on a thread that
  never entered a foreign call, and the process dies: no goroutine is there to
  run the Go function on. A library that delivers from its own threads cannot be
  bound this way;
- **a nil function clears the sink.** `SetLogCallback(nil)` calls the setter with
  a null pointer and returns no registration: nothing is registered, and the
  library stops delivering. Tell the library to forget the callback before
  `Close`, because a call that reaches a released slot panics in Go with the
  slot's number instead of delivering a record;
- **`Close` is once.** A second `Close`, or a `Close` on a nil registration,
  reports `cppgo.ErrClosed`. `Close` waits for the calls already inside the Go
  function, so none of it runs when it returns;
- **the declaration returns nothing.** The registration is the call's outcome,
  so a retained declaration whose C++ return type is a value is left out with a
  warning; a status code (`-status`) is reported as the error beside the
  registration;
- a `-retain` entry that names no declaration, or one that takes no callback, is
  an error: the flag is a statement about the C++ side, and the generator checks
  it against the declaration.

### Go memory on its way into a callback-capable call

A call that may run a callback hands Go values to code that can move them: the
callback is Go code on the goroutine that made the call, and Go code may grow
that goroutine's stack, which copies everything in it. The generated binding
therefore copies every Go value the call hands to C++ into a heap object and pins
it for the duration of the call, and copies a writable one back when the call
returns:

- a `[]T` parameter is a pinned copy, and the callee's writes reach the caller's
  slice;
- a `const char *` parameter is the NUL-terminated copy it always was, pinned;
- a `*T` parameter is a pinned copy of the value, and the callee's writes reach
  the caller's value;
- a result the thunk writes through an out-pointer is a pinned heap cell.

What is left is the shapes no copy can make safe, each with a warning: a
`std::span` or a `std::string_view` parameter (a view whose whole purpose is to
keep naming the caller's bytes), a `std::vector` the callee replaces (the
copy-back is a word of the goroutine's frame), a `void *` (nothing says what it
names, so nothing can copy it), and a standard-library result other than
`std::string`.

## Templates

A C++ **template** is a recipe for a family of classes or functions. The compiler
writes the real class or function only where a program supplies the **type
argument**; that written-out one is a **specialization**, and writing it is
**instantiating** the template. The `metrics` library has one of each kind, in
[`examples/full/include/metrics/metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp):

```cpp
// A class template. The generic Go API exists after `cppgo gen`; each
// specialization the Go program type-checks is instantiated in the shim by
// `cppgo build -scan`.
template <typename T>
class Accumulator {
public:
    Accumulator() : total_() {}
    explicit Accumulator(T start) : total_(start) {}
    ~Accumulator() {}
    T total() const { return total_; }
    void add(T value) { total_ += value; }
    static T identity(T value) { return value; }

private:
    T total_;
};

// A free function template, collected the same way.
template <typename T>
T twice(T value) {
    return value * 2;
}
```

`Accumulator<T>` is a **class template** and `twice<T>` a **function template**.
Nothing exists for either one until `T` is supplied, so the generator cannot
write a complete binding in one pass. `cppgo gen` writes a **generic** Go API:
Go declarations that take the type argument as a Go type parameter, so one API
serves every specialization. The report says so:

```
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
```

`tmpl` marks the line as a template, and `-` sits where a non-template line
carries a symbol — a name the loader resolves to an address: there is no concrete
function to point at yet. The count is the number of Go declarations the generic
API holds for the template.

The declarations `cppgo gen` writes, bodies omitted, from
[`examples/full/gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
// Accumulator is metrics::Accumulator<T>: the generic API. The concrete specializations are the ones
// the program instantiates; `cppgo build` collects them and emits the shim.
type Accumulator[T any] struct {
	obj *cppgo.Object
}

// NewAccumulator constructs a metrics::Accumulator<T>.
//
// C++: metrics::Accumulator::Accumulator<T>(T), two-phase template
func NewAccumulator[T any](start T) (*Accumulator[T], error)

// NewAccumulatorDefault constructs a metrics::Accumulator<T>.
//
// C++: metrics::Accumulator::Accumulator<T>(), two-phase template
func NewAccumulatorDefault[T any]() (*Accumulator[T], error)

// Add calls metrics::Accumulator::add.
//
// C++: metrics::Accumulator::add(T), two-phase template
func (a *Accumulator[T]) Add(value T) error

// Total calls metrics::Accumulator::total.
//
// C++: metrics::Accumulator::total() const, two-phase template
func (a *Accumulator[T]) Total() (T, error)

// Close destroys the Accumulator. It returns cppgo.ErrClosed when the receiver has no live object.
func (a *Accumulator[T]) Close() error

// AccumulatorIdentity calls metrics::Accumulator::identity.
//
// C++: metrics::Accumulator::identity(T), two-phase template
func AccumulatorIdentity[T any](value T) (T, error)

// Twice calls metrics::twice.
//
// C++: metrics::twice(T), two-phase template
func Twice[T any](arg0 T) (T, error)
```

`type Accumulator[T any]` is a **Go generic type**: `[T any]` declares a type
parameter `T` that any Go type may fill, and the generated methods use it exactly
as the C++ template uses `T`. The generic API compiles with no instantiation at
all. The two constructors are the two C++ constructors: the one with the most
parameters keeps `New<Type>`, a parameterless overload becomes
`New<Type>Default`, and the rest take the parameter suffix. `Add` and `Total`
return an error because the specialization is resolved at run time, so the call
can fail. `AccumulatorIdentity` is the static member `identity`, named
`<Type><Member>` as above. `Twice` is the free function template, and its
parameter is named `arg0` in the generated signature.

Phase two is the command that reads the program:

```console
$ cppgo build -header include/metrics/metrics.hpp -package metrics \
              -library build/libmetrics.so \
              -compile-commands build/compile_commands.json \
              -out ./gen -status metrics::validate_name \
              -scan ./cmd/... -scan-dir .
```

Run from [`examples/full`](https://github.com/Manu343726/cppgo/tree/main/examples/full)
after `make lib`, with `cppgo` on `PATH`. `-scan` loads the Go package named by
the pattern and collects every type argument passed to the generic API. The new
lines of the report are the collected specializations: the C++ specialization in
the first column, the Go type argument in the middle, and in the last column the
`template ...;` line that makes it exist.

```
metrics::Accumulator<T>                      tmpl   -                            generic API, 6 binding(s)
metrics::twice<T>                            tmpl   -                            generic API, 1 binding(s)
metrics::Accumulator<double>                 tmpl   float64                      template class ::metrics::Accumulator<double>;
metrics::Accumulator<std::int32_t>           tmpl   int32                        template class ::metrics::Accumulator<std::int32_t>;
metrics::twice<std::int64_t>                 tmpl   int64                        template class ::metrics::twice<std::int64_t>;
```

The mapping is the type map above: `int32` is `std::int32_t`, `float64` is
`double`, `int64` is `std::int64_t`. With those types known, the command writes
the class specializations into the shim. **An explicit instantiation** is a C++
declaration that makes the compiler write every member of that specialization;
the lines it added are visible in
[`gen/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/cppgo_shim/zz_cppgo_shim.cpp):

```cpp
// The specializations the Go program instantiates. cppgo build collected
// them from the program; adding one here is what makes its members exist.
template class ::metrics::Accumulator<double>;
template class ::metrics::Accumulator<std::int32_t>;
```

The free function template needs no such declaration: one thunk per collected
type is written for it — the generated `extern "C"` C++ function that carries one
call across the boundary — and the report's last column records the
specialization line cppgo derives for that type.

Finally, the Go program supplies the type arguments. This is the part of
[`cmd/app/main.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/main.go)
that uses the two templates:

```go
// The generic API: the two specializations the shim was built for.
i, err := metrics.NewAccumulator[int32](10)
if err != nil {
	return err
}
defer i.Close()
if err := i.Add(5); err != nil {
	return err
}
sum, err := i.Total()
if err != nil {
	return err
}
fmt.Printf("Accumulator[int32](10) + 5 = %v\n", sum)

d, err := metrics.NewAccumulator[float64](1.5)
if err != nil {
	return err
}
defer d.Close()
if err := d.Add(0.25); err != nil {
	return err
}
sumF, err := d.Total()
if err != nil {
	return err
}
fmt.Printf("Accumulator[float64](1.5) + 0.25 = %v\n", sumF)

twice, err := metrics.Twice[int64](21)
if err != nil {
	return err
}
fmt.Printf("twice[int64](21) = %v\n", twice)
```

Each `NewAccumulator[int32]` names the type argument at the call site; that is
what `-scan` reads. The Go package compiled before the C++ specializations
existed, and `-scan` is what makes those three calls reach C++. The full lesson
is [Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## Names

| C++ | Go |
|---|---|
| `snake_case`, `camelCase`, `PascalCase` | `CamelCase`, split on `_` |
| constructor | `New<Type>` |
| parameterless constructor of a class template | `New<Type>Default` |
| destructor | `Close` |
| copy constructor | `Clone` |
| static member `f` | `<Type>F` |
| overload | `<Name><GoTypeOfEachParameter>` |

A C++ name that maps to an existing Go name and cannot be separated by a suffix
is a generation error asking for `-rename`. A `-rename` that matches nothing is
also an error.

## The comment on every binding

Every generated declaration carries a comment naming the C++ declaration it came
from and how cppgo calls it. The **mangled name** is the name the C++ compiler
writes into the object's symbol table — namespace, class and parameter types
encoded — and a **thunk** is the generated `extern "C"` function that adapts a
call the direct path — the call that goes straight to the C++ symbol through
generated assembly — cannot carry, compiled into the shim. For
`mathx::Vector::at`, declared in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
double at(std::size_t index) const;
```

the generated comment is:

```
// C++: mathx::Vector::at(std::size_t) const, _ZNK5mathx6Vector2atEm (thunk cppgo_thunk_79aeb346bd10, not noexcept: an exception would unwind into Go), verified against libmathx.so
```

It names the declaration, its mangled symbol, the thunk that carries it, the
reason a thunk was chosen, and finally that cppgo checked the symbol against the
library's dynamic symbol table (`libmathx.so`). It is the first thing to read
when a generated method surprises you. The fields are explained in
[Architecture]({{< ref "explanation/architecture.md" >}}#generation-parse-selection-determinism).

## Related

- How to produce this package: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- How a call is made and what it costs: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- The runtime the package imports: [Runtime]({{< ref "reference/runtime.md" >}}).
