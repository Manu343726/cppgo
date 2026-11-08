# mathx

One small C++17 class, bound twice: by its header, and by nobody's hand. The
example is the executable form of
[Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/).

The C++ side (`include/mathx/vector.hpp`, `src/vector.cpp`) knows nothing about
`cppgo`: no annotations, no `extern "C"`, no binding declarations. It is built
by this directory's `Makefile` into `build/libmathx.so`, with its own flags and
its own soname, and that build compiles no generated file.

## Commands

```console
$ make        # gen, test, vet
$ make lib    # the user's library only: build/libmathx.so
$ make gen    # cppgo gen: the Go API, the assembly, the shim, the compiled shim
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

One declaration takes the direct carrier; everything else goes through the
shim. `x()` goes through the shim even though it is a trivial accessor, because
its body is inline and the library exports no symbol for it; `scale` is direct
because it is `noexcept`, not virtual, defined out of line, and the library
exports its mangled name. The rest can throw or must be copied, so the shim
carries them.

## The generated API

```go
func Open(path string) error
var CppgoLibraryPath string

type Point struct {
	X float64
	Y float64
}
func NewPoint() (*Point, error)
func (p *Point) Clone() (*Point, error)

type Vector struct{ /* unexported storage owned by C++ */ }
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
`scale` is `noexcept` and direct; `Validate` returns only an error because it
throws and has no value; `Dot` takes a slice because the declaration takes
`(pointer, length)`, and the slice is borrowed for the call.

`Midpoint` passes a 16-byte POD by value in both directions: the shim copies it
into the call and the result back into Go storage. `Saxpy` checks before the call
that its two slices have the same length, because the C++ side has one length for
both.

## Loading

The test names both objects because the loader does not search the current
directory:

- `CppgoLibraryPath` is set to `build/libmathx.so` in `init`;
- `Open` is called with the absolute path of `libcppgo_mathx.so`.

A deployment that keeps `libmathx.so` beside the shim needs neither: the shim's
`$ORIGIN` run path finds its sibling. See
[Ship a program that uses a binding](https://manu343726.github.io/cppgo/how-to/deploy/).

## Tests

`make test` runs `mathx_test.go` twice. What it asserts:

| Test | Assertion |
|---|---|
| `TestConstructorAndAccessors` | the constructor stores both components; `X`, `Y` read them back |
| `TestConstMethod` | `Norm` over a 3-4-5 triangle is 5, and 0 for the zero vector |
| `TestDirectMethod` | `Scale(2)` mutates through the direct carrier: `X`=6, `Y`=8, `Norm`=10 |
| `TestMethodTakingSlice` | `Dot` crosses as `(pointer, length)`: a two-element slice, a nil slice, and a longer one the callee clamps |
| `TestThrowingMethod` | `At(2)` and `Validate()` return `*cppgo.CallError` carrying the C++ message and naming the declaration |
| `TestPointByValue` | `Midpoint` copies a POD in and out |
| `TestSaxpy` | the two slices are written through, and a length mismatch is refused before the call |
| `TestObjectsAreIndependent` | two `Vector`s do not share storage |
| `TestClone` | the copy constructor is reachable as `Clone` |
| `TestCloseLifetime` | `Close` twice returns `cppgo.ErrClosed` |

Measured 2026-11-08: `ok ... 0.018s` with `CGO_ENABLED=0`, and `ok ... 1.034s`
under `-race`.

## The committed generated sources

`zz_cppgo.gen.go`, `zz_cppgo_amd64.s` and `cppgo_shim/` are committed on purpose:
the example is teaching material, and `make gen` reproduces them byte for byte.
`make check` is the gate: it fails when a committed file differs from what
`make gen` writes.
