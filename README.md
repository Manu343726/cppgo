# cppgo

`cppgo` generates a Go package that calls a C++ library without cgo, without
`import "C"`, and without a hand-written `extern "C"` layer. Point it at your
C++ headers and the compilation flags your build already uses; it parses them
with libclang and emits the Go API a Go programmer would have written:
`NewVector(...)`, methods, `Close()`.

**Audience:** a Go developer who has a C++ library and has never built a
foreign-function interface (FFI — the layer that lets code compiled from one
language call code compiled from another). Every C++ term this page uses is
defined in the [glossary](https://manu343726.github.io/cppgo/manual/glossary/).

## How it works, in one paragraph

Each bound declaration gets exactly one **carrier** — the way the generated Go
code reaches the C++ function. A function that is `noexcept`, not virtual, and
whose arguments fit the platform's registers is called by a generated `NOSPLIT`
assembly **trampoline** straight into the C++ ABI: no runtime handshake,
15.9 ns/op measured 2026-11-08 with `go test -bench . -benchtime 200000x`.
Everything else — virtual methods, inline bodies,
`std::string` and `std::vector`, records passed by value, templates, exceptions
— is carried by a **thunk**: a small `extern "C"` function generated into
`zz_cppgo_shim.cpp`, compiled by cppgo itself into `libcppgo_<package>.so`, and
linked against your library. Your sources and your build are never modified and
never include generated code. A signature that passes a Go function to C++ uses
a third carrier, which enters through `runtime.cgocall` so that the callback is
allowed to run.

## Quickstart

The C++ side is a header and its definitions, built by your own command:

```cpp
// include/mathx/vector.hpp
#pragma once

#include <cstddef>

namespace mathx {

class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    double norm() const;
    double dot(const double *ys, std::size_t n) const;
    void scale(double k) noexcept;

private:
    double x_, y_;
};

} // namespace mathx
```

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Wl,-soname,libmathx.so \
      -Iinclude -o build/libmathx.so src/vector.cpp
```

Ask cppgo for the Go package. The `-library` flag names your shared object, so
the generated shim is linked against it and every direct binding is
cross-checked against its dynamic symbol table:

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c482bce3e     not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, out of line, not virtual, register class
mathx::Vector::~Vector()                     thunk  cppgo_thunk_06a8bef46cac     not noexcept: an exception would unwind into Go
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

The report names the carrier and why, one line per binding. The generated
package is the API:

```go
v, err := mathx.NewVector(3, 4)
if err != nil {
    log.Fatal(err)
}
defer v.Close()

n, err := v.Norm()   // (float64, error): norm() is not noexcept
if err != nil {
    log.Fatal(err)
}
fmt.Println(n)       // 5

v.Scale(2)           // no error: scale() is noexcept, so the call is direct
fmt.Println(v.Norm()) // 10
```

That snippet is the real generated package — `NewVector`, `Norm`, `Scale` and
`Close` are the identifiers the v4 emitter produced from the header above.

### Deployment

The shim links your library by its **soname** and is linked
`-Wl,-rpath,$ORIGIN`, so the two shared objects resolve each other when they
sit in one directory. The generated `Open` looks for the user's library, in
order, at `CPPGO_LIBRARY_PATH`, `CppgoLibraryPath`, beside the shim, then on the
loader's search path:

```go
if err := mathx.Open("./mathx/libcppgo_mathx.so"); err != nil {
    log.Fatal(err)
}
```

`Open("")` uses the shim name the package was generated with, which is what a
program built with the objects on `LD_LIBRARY_PATH` wants.

For the C++ side as one file to ship, the runtime provides
`cppgo.OpenFS(fs.FS, name)`: it extracts every embedded `.so` to a private
per-user cache and loads the named one. `//go:embed` is the intended source. The
generated package's `Open` takes a path today; wiring the embedded file system
into it is [issue #20](https://git.manu343726.com/manu343726/cppgo/issues/20).

## Errors

Every call that can fail returns `(T, error)`, value first, error last; a call
that cannot fail returns no error at all. Today that covers:

- **exceptions** — a declaration that is not `noexcept` goes through a shim
  thunk that wraps the call in `try`/`catch` and puts the message in a
  thread-local string, which becomes `*cppgo.CallError` wrapping
  `*cppgo.NativeError`;
- **`-status <qualified-name>`** — a C++ function whose 32-bit return is a
  status becomes a Go function that returns only `error` (measured:
  `func Ensure() error`).

`std::error_code`, `std::expected`, `std::optional`, `errno` and the
`bool` + out-parameter convention are designed in
[`DESIGN.md`](DESIGN.md) and tracked as
[issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19); the
generator does not emit them yet.

## Requirements

| requirement | why |
|---|---|
| **Go 1.25+** | the module pins `go 1.25.0` |
| **Linux/amd64** | the generated trampolines are amd64 assembly for the System V ABI |
| **libclang** | the generator parses the headers with it (`libclang-dev`, or `CPPGO_LIBCLANG=/path/to/libclang.so`) |
| **a C++ compiler** | cppgo compiles the generated shim with it, so it must match the library's compiler family, standard library and `_GLIBCXX_USE_CXX11_ABI` (`g++`, then `clang++`, or `-cxx`) |
| **[purego](https://github.com/ebitengine/purego)** | the only third-party runtime dependency: `dlopen`/`dlsym` without cgo |

The Go side needs no C toolchain and no `import "C"`; `CGO_ENABLED=0 go build`
works. A program that loads a shared library at run time is dynamically linked
by definition — the promise is "no cgo, no C toolchain", not "one static file".

## What it is good at, and what it costs

The target use cases are C++ libraries whose API is worth keeping whole:
**Eigen** (header-only and template-heavy, so the shim is the whole binary),
**OpenCV** (hundreds of overloads, `cv::Mat`, `std::string` everywhere), and
**ArrayFire** (a C++ API over a C one, with exceptions). Bindings for those are
contract test plans, not yet shipped code.

The trade-offs, stated rather than hidden:

- **The `P` is not released during a call.** A foreign call in flight delays a
  GC cycle by up to its duration: right for short, compute-bound calls, wrong
  for calls that block. Move blocking work to another process.
- **The C++ frame lives on the goroutine stack**, inside a 32 KiB reserve
  established before every call. Deep recursion can exhaust it.
- **The binary is dynamically linked**, and the shim and the library must share
  a C++ ABI and standard library. The generator rejects a libstdc++/libc++
  mix.
- **An exception must never cross a Go frame.** A direct binding is `noexcept`
  by construction; a throw with no C++ frame above it terminates the process.
- **A callback-capable binding costs about 10× a direct one** (151 ns against
  15.9 ns, measured 2026-11-08 with `go test -bench . -benchtime 200000x`),
  because the argument block cannot live on a stack that a callback may grow.

The full list is on the [limitations
page](https://manu343726.github.io/cppgo/project/limitations/); the measured
numbers, with the command and the clock caveat, are on
[benchmarks](https://manu343726.github.io/cppgo/project/benchmarks/).

## Directory layout

| path | what lives there |
|---|---|
| [`cmd/cppgo/`](cmd/cppgo/) | the `cppgo gen`, `check`, `version` and `help` commands |
| [`internal/gen/`](internal/gen/) | the generator: selection, carrier routing, the Go and assembly emitters, the shim emitter, the shim's C++ compile |
| [`internal/cxx/`](internal/cxx/) | libclang through purego, and the C++ model (records, symbols, layouts) |
| [`internal/asm/`](internal/asm/) | the `NOSPLIT` trampolines, the stack probe, the reentrant entry |
| [`internal/callback/`](internal/callback/) | the Go→C++→Go callback table and its ABI |
| [`internal/rtl/`](internal/rtl/) | `dlopen`/`dlsym` through purego |
| [`internal/shim/`](internal/shim/) | the measured construct matrix, compiled end to end |
| [`internal/errormap/`](internal/errormap/) | the error projection prototype behind issue #19 |
| runtime root (`*.go`) | `cppgo.Open`, `OpenFS`, `Library`, `Object`, `CallReentrant`, the wire helpers and the error types |
| [`docs/adr/`](docs/adr/) | the decisions that shaped the design, one per file |
| [`site/`](site/) | the Hugo documentation site published at <https://manu343726.github.io/cppgo/> |
| [`examples/`](examples/) | runnable bindings; being regenerated against the v4 emitter in issue #4 |

## Build and test

The repository is one Go module plus a Hugo site:

```console
$ make build        # CGO_ENABLED=0 build of every package and the CLI
$ make test         # two passes: CGO_ENABLED=0, then CGO_ENABLED=1 -race
$ make cover-gate   # statement coverage, threshold 99.0 %
$ make bench        # every benchmark in the repository
$ make site         # the Hugo site, into site/public/
```

`make test` runs twice on purpose: the first pass proves the no-cgo contract,
the second is the race detector, which the Go toolchain refuses with cgo
disabled. Neither pass replaces the other. `make verify` runs everything CI
runs.

## Where the documentation lives

- **The published manual** is the Hugo site under [`site/`](site/) — tutorials,
  the manual, the reference, the examples, and the project pages. Build it with
  `make site`.
- **The decisions** are the ADRs under [`docs/adr/`](docs/adr/), each one
  immutable once accepted. The v4 contract is [`DESIGN.md`](DESIGN.md).
- **The design contract** is `DESIGN.md`: it says what the emitter must do and
  marks each row `measured` or `designed`. When a page and the code disagree,
  the code is right and the page is the bug.

## Install

```console
$ go install github.com/Manu343726/cppgo/cmd/cppgo@latest
```

From a checkout, `make build` puts the same binary in `bin/cppgo`.

## License

MIT — see [LICENSE](LICENSE).
