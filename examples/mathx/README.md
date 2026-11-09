# mathx

One small C++17 class, bound twice: by its header, and by nobody's hand. The
example is the executable form of
[Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/).

The C++ side (`include/mathx/vector.hpp`, `src/vector.cpp`) knows nothing about
`cppgo`: no annotations, no `extern "C"`, no binding declarations. It is built
by this directory's `Makefile` into `build/libmathx.so`, with its own flags and
its own soname, and that build compiles no generated file.

## The header

[`include/mathx/vector.hpp`](include/mathx/vector.hpp) is the input of the whole
example. These are the declarations `make gen` reads:

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

## Commands

```console
$ make        # gen, test, vet
$ make lib    # the user's library only: build/libmathx.so
$ make gen    # cppgo gen: the Go API, the C++ side, and the compiled C++ side
$ make test   # CGO_ENABLED=0, then CGO_ENABLED=1 -race
$ make check  # cppgo check: are the committed generated sources current?
$ make clean
```

Requirements: Go 1.25+, a C++17 compiler, libclang 20+ (generation only).

## What is generated

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

The report has one line per declaration. What its columns mean, and why one
declaration reads `direct`, is in
[Architecture](https://manu343726.github.io/cppgo/explanation/architecture/) and
[Performance](https://manu343726.github.io/cppgo/explanation/benchmarks/).

## The generated API

`make gen` turned the header above into
[`zz_cppgo.gen.go`](zz_cppgo.gen.go). Its declarations are:

```go
type Point struct {
	X float64
	Y float64
}
func NewPoint() (*Point, error)
func (p *Point) Clone() (*Point, error)

type Vector struct {
	obj *cppgo.Object
}
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Clone() (*Vector, error)
func (v *Vector) Close() error
func (v *Vector) X() (float64, error)
func (v *Vector) Y() (float64, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Dot(ys []float64) (float64, error)
func (v *Vector) At(index uint64) (float64, error)
func (v *Vector) Validate() error
func (v *Vector) Scale(k float64)

func Midpoint(a Point, b Point) (Point, error)
func Saxpy(a float64, x []float64, y []float64) error
func VectorDims() (int32, error)
```

Read the error results as a statement about the C++: `Scale` has none because
`scale` cannot fail; `Validate` returns only an error because it can fail and has
no value; `Dot` takes a slice because the declaration takes `(pointer, length)`,
and the slice is borrowed for the call.

`Midpoint` passes a 16-byte struct by value in both directions: a `Point` is
copied into the call and the result is a new value. `Saxpy` checks before the
call that its two slices have the same length, because the C++ side has one
length for both.

## The call

A call from [`mathx_test.go`](mathx_test.go), where `scale` is the direct carrier
and `X` reads the result back:

```go
v, err := NewVector(3, 4)
if err != nil {
	t.Fatalf("NewVector(3, 4): %v", err)
}
defer v.Close()

v.Scale(2)

if got, err := v.X(); err != nil || got != 6 {
	t.Errorf("X() after Scale(2) = %v, %v, want 6, nil", got, err)
}
```

## Loading

The test names both objects because the loader does not search the current
directory:

- `CppgoLibraryPath` is set to `build/libmathx.so` in `init`;
- `Open` is called with the absolute path of `libcppgo_mathx.so`.

A deployment that keeps `libmathx.so` beside the generated file needs neither:
the two find each other. See
[Ship a program that uses a binding](https://manu343726.github.io/cppgo/how-to/deploy/).

## Tests

`make test` runs `mathx_test.go` twice. What it asserts:

| Test | Assertion |
|---|---|
| `TestConstructorAndAccessors` | the constructor stores both components; `X`, `Y` read them back |
| `TestConstMethod` | `Norm` over a 3-4-5 triangle is 5, and 0 for the zero vector |
| `TestDirectMethod` | `Scale(2)` mutates the object: `X`=6, `Y`=8, `Norm`=10 |
| `TestMethodTakingSlice` | `Dot` crosses as `(pointer, length)`: a two-element slice, a nil slice, and a longer one the callee clamps |
| `TestThrowingMethod` | `At(2)` and `Validate()` return `*cppgo.CallError` carrying the C++ message and naming the declaration |
| `TestPointByValue` | `Midpoint` copies a `Point` in and out |
| `TestSaxpy` | the two slices are written through, and a length mismatch is refused before the call |
| `TestObjectsAreIndependent` | two `Vector`s do not share storage |
| `TestClone` | the copy constructor is reachable as `Clone` |
| `TestCloseLifetime` | `Close` twice returns `cppgo.ErrClosed` |

## The committed generated sources

`zz_cppgo.gen.go`, `zz_cppgo_amd64.s` and `cppgo_shim/` are committed on purpose:
the example is teaching material, and `make gen` reproduces them byte for byte.
`make check` is the gate: it fails when a committed file differs from what
`make gen` writes.
