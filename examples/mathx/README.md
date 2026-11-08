# mathx — one small class, header-first

`mathx::Vector` is a C++17 class with a constructor, a destructor, an inline
`const` accessor, ordinary `const` methods, a method that takes a slice, a method
that throws, a `noexcept` mutator and a static member. Beside it, `Point` is a
16-byte POD passed by value and `saxpy` is the BLAS-shaped free function.

None of that is annotated. The header and the source are an ordinary C++
library; one `cppgo gen` reads the header and writes the Go API, the assembly
trampolines and the C++ shim, then compiles the shim itself. Go calls the C++
with no `import "C"` and no cgo at build time.

```console
$ cd examples/mathx
$ make
```

That builds `build/libmathx.so` with the project's own command, generates the
binding, and runs the tests twice — with `CGO_ENABLED=0` and with
`CGO_ENABLED=1` under the race detector — then `go vet`.

## The three artefacts

The generated files are committed, so you can read what the generator produced
before running anything:

| artefact | file | who builds it |
|---|---|---|
| the user's library | `build/libmathx.so` | this example's Makefile, from `src/` and `include/` |
| the cppgo shim | `cppgo_shim/zz_cppgo_shim.{h,cpp}`, compiled to `libcppgo_mathx.so` | `cppgo gen`, linked against the user's library |
| the generated Go package | `zz_cppgo.gen.go`, `zz_cppgo_amd64.s` | `cppgo gen` |

The dotted line between the first row and the other two is the contract: **the
user's build is untouched, and no generated code is in it.** The Makefile's
`lib` target compiles `src/vector.cpp` and nothing else.

## What the generator reported

`make gen` prints one line per declaration, naming the **carrier** it chose:

```
mathx::Vector::Vector(const Vector &)        thunk  cppgo_thunk_1af9…  not noexcept: an exception would unwind into Go
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c…  not noexcept: an exception would unwind into Go
mathx::Vector::at(std::size_t) const         thunk  cppgo_thunk_79ae…  not noexcept: an exception would unwind into Go
mathx::Vector::dims()                        thunk  cppgo_thunk_f4ea…  not noexcept: an exception would unwind into Go
mathx::Vector::dot(const double *, std::size_t) const thunk  cppgo_thunk_9e44…  not noexcept: an exception would unwind into Go
mathx::Vector::norm() const                  thunk  cppgo_thunk_4999…  not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd  noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::x() const                     thunk  cppgo_thunk_0006…  not noexcept: an exception would unwind into Go
mathx::Vector::y() const                     thunk  cppgo_thunk_2507…  not noexcept: an exception would unwind into Go
mathx::Vector::~Vector()                     thunk  cppgo_thunk_06a8…  not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a666…  a record passed or returned by value: the thunk copies it
mathx::saxpy(double, const double *, double *, std::size_t) thunk  cppgo_thunk_edd1…  not noexcept: an exception would unwind into Go
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

Two carriers, one reason each:

- **`direct`** calls `_ZN5mathx6Vector5scaleEd` in `libmathx.so` from a NOSPLIT
  trampoline. Only `scale` qualifies: it is `noexcept`, defined out of line (so
  the symbol exists), not virtual, and its arguments fit the System V registers.
- **`thunk`** calls `cppgo_thunk_…` in `libcppgo_mathx.so`, which calls the
  declaration as C++ and converts what C++ cannot hand across the ABI. An
  inline body (`x`), a non-`noexcept` function (`norm`, `at`), a by-value record
  (`midpoint`) and a destructor are all shim work.

`x()` is the one that would have been impossible before v4: its body lives
inside the class, so the library exports no symbol for it, but the shim includes
the header and compiles the body into itself.

## The generated API

These are the declarations in [`zz_cppgo.gen.go`](zz_cppgo.gen.go), not an
illustration:

```go
func Open(path string) error

// Point is mathx::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.
type Point struct {
	X float64
	Y float64
}

func NewPoint() (*Point, error)
func (p *Point) Clone() (*Point, error)

// Vector is mathx::Vector. The object belongs to C++; obj is where it lives.
type Vector struct{ /* unexported */ }

func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Clone() (*Vector, error)
func (v *Vector) Close() error

func (v *Vector) X() (float64, error)      // inline in the header: shim
func (v *Vector) Y() (float64, error)      // shim
func (v *Vector) Norm() (float64, error)   // may throw: shim
func (v *Vector) Dot(ys []float64) (float64, error)
func (v *Vector) At(index uint64) (float64, error) // throws: shim
func (v *Vector) Validate() error          // void and throwing: shim
func (v *Vector) Scale(k float64)          // noexcept: direct, no error
func VectorDims() (int32, error)

func Midpoint(a Point, b Point) (Point, error) // Point by value
func Saxpy(a float64, x []float64, y []float64) error
```

Three rules are visible in that list:

- **no error is invented for a call that cannot fail.** `Scale` is `noexcept`,
  so its Go signature is `func(k float64)` — not `(error)`, and no `error` that
  is always nil.
- **a call that can fail returns the value first and an `error` last.** `At` is
  declared without `noexcept`, so the shim wraps it in `try`/`catch` and the
  message the C++ exception carries arrives as a `*cppgo.CallError`.
- **ownership is explicit.** `NewVector` returns an owning `*Vector`; `Close`
  runs the destructor; a nil or closed value returns `cppgo.ErrClosed`, and the
  direct method, which has no error result, panics with the declaration's name.

There is no `cppgo.Handle` in any signature. The object pointer is unexported
state held in a `*cppgo.Object`, which pins the storage C++ writes into.

## Walking the interesting paths

### The inline accessor and the direct method

`X` is defined inside the class body in the header. It is a shim call, and
`TestConstructorAndAccessors` exercises it. `Scale` is noexcept and out of line,
so `TestDirectMethod` reaches the library's own symbol.

### The throwing method

`at()` throws `std::out_of_range` for an index other than 0 or 1:

```go
_, err := v.At(2)
if err != nil {
	var call *cppgo.CallError
	errors.As(err, &call) // call.Symbol == "mathx::Vector::at"
	// err.Error() contains "Vector::at: index out of range"
}
```

The shim catches the exception, stores its `what()` on the calling thread, and
returns a non-zero status. The generated binding reads that status, copies the
message out and returns `*cppgo.CallError` wrapping it. `TestThrowingMethod`
asserts both the text and the symbol.

`validate()` is the same mechanism with no value: it throws `std::runtime_error`
for the zero vector, so its Go signature is only `error`. That makes the pair
`Scale` (void, `noexcept`, no error) and `Validate` (void, throws, only an
error) the clearest reading of the rule that a call which cannot fail gets no
error result.

### A POD by value

`Point` is a 16-byte POD, so it is *mirrorable*: the generated Go struct has the
same fields, at the same offsets, and `zz_cppgo.gen.go` proves the size,
alignment and offsets at compile time with `unsafe.Sizeof` assertions.

```go
mid, err := Midpoint(Point{X: 0, Y: 0}, Point{X: 4, Y: 8}) // Point{X: 2, Y: 4}
```

The shim copies the value into the call and the result back into Go storage;
`TestPointByValue` checks the arithmetic.

### Slices are (pointer, length)

`Vector::dot(const double *ys, std::size_t n)` becomes `Dot(ys []float64)`. The
slice crosses as two arguments and is borrowed for the call; nothing is copied.
`Saxpy(a, x, y)` does the same for two slices and refuses a length mismatch in
Go before the call.

### Lifetime and the zero value

```go
v, err := NewVector(3, 4)
if err != nil { ... }
defer v.Close()

err = v.Close()                 // the second one
errors.Is(err, cppgo.ErrClosed) // true

_, err = v.X()                  // ErrClosed: the object is gone
```

`Close` is not idempotent-by-silence: a second call reports `cppgo.ErrClosed`
instead of running the destructor twice. `TestCloseLifetime` checks the second
close, a method on the closed value, and the panic of the direct method (the one
binding with no error result to return).

## Where the shared objects come from at run time

The generated package loads two objects:

1. `libmathx.so`, the user's library;
2. `libcppgo_mathx.so`, the shim, which names the first in its `DT_NEEDED`
   entry by its **soname** — that is why the Makefile links with
   `-Wl,-soname,libmathx.so`.

The loader does not search the current directory. The test sets both paths
explicitly, which is also the deployment story:

```go
CppgoLibraryPath = "/opt/mathx/lib/libmathx.so" // read before the first Open
mathx.Open("/opt/mathx/lib/libcppgo_mathx.so")
```

`CPPGO_LIBRARY_PATH` overrides the first from the environment, and
`CPPGO_MATHX_LIB` / `CPPGO_MATHX_SHIM` are what the Makefile passes the test.
Opening the library before the shim is what makes the shim's `DT_NEEDED` name
resolve to the object this process already holds; the shim also carries
`-Wl,-rpath,$ORIGIN`, so the default deployment — both files in one directory —
needs no environment at all.

## Layout

```
examples/mathx/
├── include/mathx/vector.hpp    the declarations: the source of truth
├── src/vector.cpp              the definitions; compiled into the user's library
├── .clangd                     the project's flags for the parser (-std=c++17, -Iinclude)
├── Makefile                    lib, gen, test, vet, check, clean
├── mathx_test.go               assertions against the generated API
├── zz_cppgo.gen.go             generated: the Go API, Open, the trampolines' declarations
├── zz_cppgo_amd64.s            generated: one NOSPLIT trampoline per binding
├── cppgo_shim/                 generated: the shim's C++ sources
├── libcppgo_mathx.so           built by cppgo gen (ignored, not committed)
├── go.mod                      its own module, replace ../..
└── build/                      ignored: libmathx.so and the locally built cppgo
```

The generated **sources** are committed on purpose: they are the teaching
material. `make gen` reproduces them and `make check` proves they are current:

```console
$ make check
build/cppgo check -header include/mathx/vector.hpp -package mathx -library build/libmathx.so -out .
.: generated files are up to date
```

## The `.clangd`

`cppgo gen` parses the header with the flags the project actually builds with.
This example states them the way clangd does, in [`.clangd`](.clangd):

```yaml
CompileFlags:
  Add: [-std=c++17, -Iinclude]
```

Without it the generator falls back to `-std=c++17 -I<directory of the header>`
and says the flags were an assumption. A project with a
`compile_commands.json` can pass `-compile-commands` instead; the example uses
`.clangd` because it is one file and it is the same configuration an editor
reads.

## Requirements

- Go 1.25 or newer.
- A C++17 compiler (`make CXX=clang++` works too).
- libclang, because `cppgo gen` parses the header with it: on Debian/Ubuntu
  `sudo apt install clang libclang-dev`, on Arch `sudo pacman -S clang`.

The Makefile builds its own `cppgo` from `../../cmd/cppgo` into `build/cppgo`, so
no global install is needed.

## Other targets

```console
$ make lib      # just the user's library
$ make gen      # just the binding (Go API, assembly, shim sources, shim .so)
$ make test     # go test, CGO_ENABLED=0 then -race
$ make vet      # go vet, which also checks the generated assembly declarations
$ make check    # cppgo check: are the committed generated sources current?
$ make clean
```
