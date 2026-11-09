# cppgo

cppgo gives a Go program an ordinary Go package that calls a C++ library. A
**binding** is the Go code that calls a library written in another language;
cppgo writes that code instead of you. You give it two things: the library's
**header** — the C++ file a program includes to see its classes and functions —
and the **compilation configuration** for that header: the directories your C++
compiler searches for included files and the language standard it compiles with.
It writes the package that calls the library; this page calls it the **generated
package**.

Your C++ sources, your build and your library are not modified, and the
generated package needs **no cgo at build time and no `import "C"`**. cgo is
Go's built-in way to call C: it needs a C compiler at build time and a special
`import "C"` block, which lets Go compile C code written beside it, and it
carries a per-call cost. cppgo's generated package loads the compiled library at
run time through a small Go library — the **runtime** — that does not depend on
cgo.

It is a generator (`cppgo gen`) plus that runtime. `cppgo gen` reads the header
with **libclang** — the C library of the Clang compiler front end, which parses
C++ — and writes the package. libclang and a C++ compiler are needed to
*generate*. The program you ship needs neither.

**Status.** The generator, the runtime and the examples are implemented and
tested, including three example modules that bind a slice of OpenCV, ArrayFire
and Eigen and test it against the installed library. The full set of C++ failure
conventions and the remaining C++ construct support are designed and not
shipped; each is marked where it appears in the documentation.

## Quick start

You need:

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.25 or newer ([`go.mod`](https://github.com/Manu343726/cppgo/blob/main/go.mod): `go 1.25.0`) | building the generator, the runtime and your program |
| libclang | **20 or newer** | `cppgo gen` and `cppgo check` only |
| A C++ compiler | C++17; the same family and standard library as your library | generating |
| `make` | any | the repository's targets |

libclang is the C library of the Clang compiler front end. `cppgo gen` uses it to
read the header; the program you ship does not link it. Version 20 is the floor
because older releases lack a function the generator uses.

The input is an ordinary C++ header — no annotations, no `extern "C"` block (the
C++ way to expose a function to C), no binding declarations. This is the class
the quick start binds,
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
#include <cstddef>

namespace mathx {

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

} // namespace mathx
```

Build the generator, then run the example's generation step:

```console
$ make build                     # writes bin/cppgo, with CGO_ENABLED=0
$ make -C examples/mathx gen     # builds build/libmathx.so, then generates
```

`make build` writes the `cppgo` command you use on your own project. The example
builds its own copy under `examples/mathx/build/`, so its generation command,
run from inside `examples/mathx`, is:

```console
$ build/cppgo gen -header include/mathx/vector.hpp -package mathx \
                  -library build/libmathx.so -out .
```

`cppgo gen` reads the header with the include paths and language standard your
project uses: from a `.clangd` file beside the sources, or from the
`compile_commands.json` that CMake and Meson write. It reports one line per
function or method it bound, and writes the Go package.

These are the declarations that class became, copied from the generated
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
func (v *Vector) Close() error
```

`Norm` returns an error because `mathx::Vector::norm()` can throw a C++
exception — C++'s way of reporting failure. `Scale` returns nothing because
`mathx::Vector::scale(double) noexcept` is marked `noexcept`: it promises not to
throw, so there is no error to check.

The package it wrote is ordinary Go. Import it as `mathx` and call it:

```go
v, err := mathx.NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

norm, err := v.Norm()
if err != nil {
	log.Fatal(err)
}
fmt.Println(norm)          // 5

v.Scale(2)                 // no error to check: this call cannot fail
```

Run the example's tests:

```console
$ make -C examples/mathx test
```

Both passes print `ok` for the package: the first builds the generated package
and the runtime with `CGO_ENABLED=0`, and the second runs the same tests under
the race detector, which needs cgo.

The full walkthrough is
[Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/).

## What the generated package looks like

The package mirrors the C++ API with Go types, and every C++ rule about failure
shows up as a Go signature you can read:

- a constructor returns `(*Type, error)`, and `Close` releases the object. There
  is no finalizer, so closing is your job.
- a method that can fail returns `(T, error)`; a method that cannot fail returns
  its value with no error at all.
- a plain C++ struct whose fields have a fixed layout becomes a Go struct with
  the same fields.
- a free function (one not inside a class), a static member function (one that
  belongs to the class rather than to an object) and an overload (one of several
  functions sharing a name with different arguments) become package functions.
- a Go slice stands for a `(pointer, length)` pair: the C++ side reads the
  slice's memory during the call, nothing is copied, and the generated code
  keeps the slice alive until the call returns.

The exact shapes, per declaration, are in
[Generated API](https://manu343726.github.io/cppgo/reference/generated-api/).

## Performance

cppgo exists partly because of what cgo costs per call, so the comparison is
measured rather than claimed. The benchmark suite runs the same C++ function
through cgo and through a generated package, plus a plain-Go baseline, in one
process: [Performance](https://manu343726.github.io/cppgo/explanation/benchmarks/)
has the tables, the code of every arm and the method.

What it shows:

- **A call the generator can make directly is about 2.8 times faster than cgo**
  on the same arithmetic: 10.5 ns against 29.6 ns.
- **A call that has to report a status costs about what cgo costs**: 29.2 ns
  against 29.6 ns on the same arithmetic.
- **Where the C++ work dominates, the cost of crossing from Go to C++ stops
  mattering**: a 1024-element vector in and out is within about 10% of cgo.
- **Several calls into C++ standard-library types are faster than cgo**: a string
  in and out, an eight-element vector, a successful status call, a method on a
  live object.
- **Callbacks and object creation and close are slower than cgo's**; the page
  names the causes.

The value proposition is the build (no cgo, no `import "C"`, your library
untouched) plus a call the generator can make directly that is faster than cgo
and a status call that lands near its price, not an order-of-magnitude win on
every operation. The suite is in
[`benchmarks/`](https://github.com/Manu343726/cppgo/tree/main/benchmarks), and
its raw results are committed beside it.

## Errors

**A call that can fail returns `(T, error)`; a call that cannot fail returns no
error.** Two C++ failure conventions reach Go today:

- **exceptions** — a C++ function that throws (the language's way of reporting
  failure) becomes a Go call that returns an error. The Go error is a
  `*cppgo.CallError` naming the C++ declaration, wrapping a `*cppgo.NativeError`
  that carries the library's message. `errors.As` finds either one.
- **status codes** — mark a declaration with `-status` and its 32-bit return
  becomes an error: the Go signature returns only the error, and a non-zero value
  is the failure.

Designed and not shipped: the other failure conventions — a standard C++ error
value (`std::error_code`), a value-or-error type (`std::expected`), the C error
number `errno`, a variant (`std::variant`, a type that holds one of several
alternatives), a success flag with out-parameters (values written through
pointers), and a project's own result type. Until then, a function returning
`std::error_code` is refused at generation time with a message that says what to
do.

[Report failures from C++ to Go](https://manu343726.github.io/cppgo/how-to/errors/)
has the error types, the `errors.As` examples and the designed table.

## Deployment

cppgo compiles a small C++ library for you at generation time. It is the
**shim**: generated C++ that makes the calls the generated package cannot make
directly, compiled into `libcppgo_<package>.so` and linked against your library.
Your program loads two **shared objects** at run time — the shim and your own
library. A shared object is a compiled library file (`.so` on Linux) that the
system loader opens into the process when the program asks for it.

For deployment:

- keep the two shared objects in one directory, and open the shim by a path that
  contains a slash (`./libcppgo_mathx.so`), because the loader does not search
  the current directory; or
- put them on the loader's search path (`LD_LIBRARY_PATH` on Linux); or
- name your library explicitly with `CppgoLibraryPath` or the
  `CPPGO_LIBRARY_PATH` environment variable.

`cppgo.OpenFS` lets the two shared objects travel inside the Go binary instead:
it embeds them with `//go:embed` (Go's directive for embedding files in the
binary), extracts them to a private per-user cache and loads them from there. The
runtime implements it; the `Open` that `cppgo gen` writes does not call it yet,
so embedding is currently a runtime API.

[Ship a program that uses a binding](https://manu343726.github.io/cppgo/how-to/deploy/)
has the steps and the failure messages;
[Embed the C++ side in the Go binary](https://manu343726.github.io/cppgo/how-to/embed/)
covers embedding and its limits.

## Target libraries

Three example modules bind a slice of a real, large C++ library. Each is a Go
module of its own under [`examples/`](https://github.com/Manu343726/cppgo/tree/main/examples),
generated by `cppgo gen` from the library's installed headers, with a test suite
that asserts values against the library itself:

| Target | The module | What the slice binds |
|---|---|---|
| **OpenCV** | [`examples/opencv`](https://github.com/Manu343726/cppgo/tree/main/examples/opencv) ([README](https://github.com/Manu343726/cppgo/blob/main/examples/opencv/README.md)) | the `cv::NormTypes` and `cv::TermCriteria::Type` enums, the `cv::TermCriteria` class (both constructors, `isValid`, three fields), `cv::TickMeter`, and the scalar functions of `core.hpp`: version, tick frequency, thread count, CPU count and the optimization flag |
| **ArrayFire** | [`examples/arrayfire`](https://github.com/Manu343726/cppgo/tree/main/examples/arrayfire) ([README](https://github.com/Manu343726/cppgo/blob/main/examples/arrayfire/README.md)) | the `af_dtype` and `af_source` enums, and the functions that report the library's configuration: `getDevice`, `getDeviceCount`, `getMemStepSize`, `getSizeOf`, `isDoubleAvailable`, `isHalfAvailable` |
| **Eigen** | [`examples/eigen`](https://github.com/Manu343726/cppgo/tree/main/examples/eigen) ([README](https://github.com/Manu343726/cppgo/blob/main/examples/eigen/README.md)) | the `StorageOptions`, `ComputationInfo` and `UpLoType` enums, and the thread functions `initParallel`, `nbThreads` and `setNbThreads`; Eigen is header-only, so the shim is the whole binary |

Each module's README lists every declaration it binds and every one it refuses,
with the generator's message.

What a slice binds is what the generator carries today, not the whole library.
`cv::Mat`, `af::array` and Eigen's matrix types are not bound by these modules,
so no image is loaded or converted, no array is created, reduced or multiplied,
and no matrix product is computed through them. A class binds only up to what
can cross: `cv::Mat` and `af::array` come out without their constructors — there
is no `NewMat` or `NewArray` — because each declares one member the generator
cannot carry, and an Eigen matrix specialization cannot be instantiated for the
generator to read at all.
[Limits](https://manu343726.github.io/cppgo/explanation/limits/) names each
refusal.

## Limits

The full list, with the test that measures each one, is
[Limits](https://manu343726.github.io/cppgo/explanation/limits/). The ones that
shape day-to-day use:

- **These C++ constructs are refused for now**: an enum (a named set of integer
  constants), a class that inherits from another, an operator such as `+` or `==`
  defined for a C++ type, a template declared inside a class, a smart pointer (a
  C++ type that owns and frees an object automatically), and a variadic function
  (one taking a variable number of arguments). Each is a generation error naming
  the declaration and a workaround; the full list is the linked page.
- **a Go function passed to C++ as a callback takes at most six integer and eight
  floating-point arguments.**
- **`long double` and `__int128` have no Go type of the same width; `void *` and
  `std::nullptr_t` are refused.**
- **a macro-only API cannot be bound**: libclang sees the expansion of a macro (a
  name the compiler replaces with text before parsing), not its declaration.
- **`char *` parameters do not bind in this build.** Use `std::string` or
  `std::string_view` (a non-owning view of a string) at the C++ boundary.
- **the shim cppgo builds must match your library's compiler family and standard
  library.** The two call each other across a compiled interface — the ABI, the
  layout of arguments and results — and nothing detects a mismatch for you.

## Build and test

```console
$ make build       # CGO_ENABLED=0 go build ./..., then the command into bin/
$ make test        # two passes: CGO_ENABLED=0 go test, then CGO_ENABLED=1 -race
$ make lint        # go vet, then golangci-lint when it is installed
$ make cover-gate  # statement coverage >= 99% on ./...
$ make examples    # build, generate and test every example
$ make site        # build the documentation site with hugo
```

`make test` runs two passes because the race detector needs cgo, and the
toolchain rejects `-race` with `CGO_ENABLED=0`. The first pass proves the no-cgo
property; the second finds data races. `make verify` runs build, lint, test,
coverage and examples in one shot.

## Documentation

The manual is published at <https://manu343726.github.io/cppgo/> and built from
[`site/`](https://github.com/Manu343726/cppgo/tree/main/site). It follows four
sections: tutorials, how-to guides, reference and explanation —
[Performance](https://manu343726.github.io/cppgo/explanation/benchmarks/) is the
cost comparison, and
[Architecture](https://manu343726.github.io/cppgo/explanation/architecture/) is
where *how it works* is written down.

[`examples/`](https://github.com/Manu343726/cppgo/tree/main/examples) is the
executable half of the tutorials: each example owns its build, and its
`Makefile` targets are the commands the tutorials use.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). In short: `make verify` must pass, no
change may add cgo to the runtime, and a generated file is never edited by hand.

## Licence

MIT. See [`LICENSE`](https://github.com/Manu343726/cppgo/blob/main/LICENSE).
