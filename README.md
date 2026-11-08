# cppgo

cppgo generates a Go package from a C++ header. You point it at the header and at
the way your project compiles it; it writes the Go API, one assembly trampoline
per call, and a C++ shim that it compiles itself. The generated package calls
into your library with **no cgo at build time and no `import "C"`**, and your
library, your sources and your build are not modified.

It is a generator (`cppgo gen`) plus a small runtime that the generated package
imports. The runtime has one third-party dependency, `github.com/ebitengine/purego`,
used for `dlopen`/`dlsym` only. libclang and a C++ compiler are needed to
*generate*; a program built with a generated package needs neither.

**Status.** The generator, the runtime, the shim build and the two examples are
in the tree and tested. The full error projection, the wider
operators/inheritance work and the OpenCV, ArrayFire and Eigen target slices are
designed and not shipped; each is marked where it appears in the documentation.

## The workflow

```
your header + your compilation configuration
        │
        ▼
   cppgo gen ──► zz_cppgo.gen.go   (the Go API)
        │        zz_cppgo_amd64.s  (one trampoline per binding)
        │        cppgo_shim/*.cpp  (the thunks)
        │        libcppgo_<pkg>.so (the shim, compiled by cppgo)
        ▼
   your Go program
```

1. You write ordinary C++. No annotations, no `extern "C"` block, no binding
   declarations.
2. Your build compiles it into its own shared object, with its own flags. cppgo
   is not involved in that build and no generated file is compiled into it.
3. `cppgo gen` reads the header through libclang, using the include paths and
   language standard from `compile_commands.json` or `.clangd`, and writes a Go
   package plus the shim.
4. cppgo compiles the shim itself and links it against your library. The
   generated package loads the shim; the shim's `DT_NEEDED` entry brings your
   library in.

## Requirements

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.25 or newer (`go.mod`: `go 1.25.0`) | building the runtime, the generator and the generated package |
| libclang | **20 or newer** | `cppgo gen` and `cppgo check`. Reading base-class offsets uses `clang_getOffsetOfBase`, which libclang 19 does not export |
| A C++ compiler | C++17; same family and standard library as your library | compiling the shim |
| `make` | any | the repository's targets (`make build`, `make test`, `make examples`) |

libclang is a generation-time dependency. The runtime never imports the parser
and has no libclang dependency; a test enforces that split.

## Quick start

Build the generator and run the example, which is a small C++17 class. The
example builds its own `cppgo`, so `bin/cppgo` is the one you use on your own
project:

```console
$ make build                     # bin/cppgo, with CGO_ENABLED=0
$ make -C examples/mathx gen     # build libmathx.so, generate, compile the shim
```

The example's `Makefile` runs this generation command, with its own freshly
built `cppgo` and paths relative to the example:

```console
$ build/cppgo gen -header include/mathx/vector.hpp -package mathx \
                  -library build/libmathx.so -out .
```

It prints one line per binding — the C++ declaration, its carrier, its symbol and
the reason — and then the compiler it used for the shim. The package it wrote is
ordinary Go:

```go
v, err := mathx.NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

norm, err := v.Norm()      // C++: mathx::Vector::norm() const
if err != nil {
	log.Fatal(err)
}
fmt.Println(norm)          // 5

v.Scale(2)                 // C++: mathx::Vector::scale(double) noexcept
```

`Norm` returns `(float64, error)` because the C++ method may throw. `Scale`
returns nothing because it is `noexcept`, so the generated signature has no
error result at all. Run it:

```console
$ make -C examples/mathx test
ok  	github.com/Manu343726/cppgo/examples/mathx	0.018s
ok  	github.com/Manu343726/cppgo/examples/mathx	1.034s   # -race
```

The full walkthrough, including what each generated file contains and what the
report means, is [Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/).

## The three artefacts

| Artefact | Owner | What it is |
|---|---|---|
| your library | you | your sources, your flags, your build. cppgo never writes into it |
| the cppgo shim | cppgo | `zz_cppgo_shim.cpp` and its header, generated from the same parse as the Go API, compiled by cppgo into `libcppgo_<package>.so` and linked against your library |
| the generated Go package | cppgo | the API you call, one `NOSPLIT` trampoline per binding, and the code that loads the shim |

A header-only library has no shared object at all: the shim is the whole binary.

## Carriers

Every binding is assigned exactly one **carrier** before anything is emitted. The
carrier is invisible in the API, named in the report and in the binding's
evidence comment.

| Carrier | When | Cost |
|---|---|---|
| **direct** | `noexcept`, not virtual, defined out of line, registers representable, symbol exported by the library | 15.9 ns/op |
| **thunk** | everything else: inline bodies, virtual members, `std::` types, by-value records, constructors, destructors, anything that can throw | one C++ frame inside your library |
| **reentrant** | the signature passes a Go function to C++ | 151 ns/op, one 16-byte allocation |

Measured 2026-11-08 with `go test -bench . -benchtime 200000x`; the numbers and
the machine are recorded in
[`DESIGN.md`](https://github.com/Manu343726/cppgo/blob/main/DESIGN.md) and the
[carrier table](https://manu343726.github.io/cppgo/reference/carriers/).

## Errors

**A call that can fail returns `(T, error)`; a call that cannot fail returns no
error.** Today the generator projects two C++ conventions:

- **exceptions** — a declaration that is not `noexcept` goes through a thunk that
  catches `std::exception` (and `...`), stores the message, and returns it as a
  `*cppgo.CallError` wrapping a `*cppgo.NativeError`. `errors.As` finds both.
- **status codes** — a declaration named with `-status` has its 32-bit return
  treated as a status: the Go signature returns only an error, and a non-zero
  value is the failure.

Designed and not shipped ([issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19)):
`std::error_code`, `std::expected`, `errno`, `std::variant`, a configured `bool`
plus out-parameters, and custom result types. The projection is implemented and
tested in `internal/errormap`; the generator does not consume it yet, and a
function returning `std::error_code` is currently a generation error.

One rule cannot be configured away: an exception thrown with no C++ frame above
it terminates the process. That is why a throwing declaration is never direct.
See [Report failures from C++ to Go](https://manu343726.github.io/cppgo/how-to/errors/).

## Deployment

A program that uses a binding loads two shared objects: the shim and your
library. Keep them in one directory and the shim's `-Wl,-rpath,$ORIGIN` finds
its sibling; or name the library with `CppgoLibraryPath` /
`CPPGO_LIBRARY_PATH`; or put the directory on the loader's search path.

`cppgo.OpenFS` embeds the objects in the Go binary and extracts them to a private
per-user cache (`0700`/`0600`, atomic renames, content-addressed). It is
implemented and measured in the runtime; a *generated* package's `Open` does not
call it yet, so embedding is currently a runtime API, not a generated-package
feature. See [Ship a program that uses a binding](https://manu343726.github.io/cppgo/how-to/deploy/)
and [Embed the C++ side in the Go binary](https://manu343726.github.io/cppgo/how-to/embed/).

## Target libraries

The design is exercised against three libraries. **None of the three is
implemented yet**; they are the acceptance tests the contract describes, each
with a build tag and a skip when the library is absent.

| Target | What it exercises |
|---|---|
| **Eigen** | the two-phase template flow; a header-only library where the shim is the whole binary; by-value fixed-size matrices |
| **OpenCV** | whole-header binding at scale: hundreds of overloads, `cv::Mat` as an opaque value, `std::string`/`std::vector` everywhere |
| **ArrayFire** | a C++ API over a C API; opaque handles; `af::exception` caught by the shim and reported as Go errors |

## Limits

The full list, with the test that measures each one, is
[Limits](https://manu343726.github.io/cppgo/explanation/limits/). The ones that
shape day-to-day use:

- **enums, inheritance, operators, member templates, smart pointers and
  variadic functions are not emitted yet.** Each is a generation error naming the
  declaration and a workaround.
- **a callback carries at most six INTEGER and eight SSE arguments.** It is
  called through the reentrant carrier; on the direct trampoline it kills the
  process.
- **`long double`, `__int128`, `void *` and `nullptr_t` have no Go value type.**
- **a macro-only API cannot be bound**, because libclang sees a macro's
  expansion, not its declaration.
- **`char *` parameters do not bind in this build.** The generated shim fails to
  compile; use `std::string` or `std::string_view` at the boundary.
- **the shim and your library must share a C++ ABI and standard library.**

## Build and test

```console
$ make build       # CGO_ENABLED=0 go build ./..., then every cmd/ binary into bin/
$ make test        # two passes: CGO_ENABLED=0 go test, then CGO_ENABLED=1 -race
$ make lint        # go vet, then golangci-lint when it is installed
$ make cover-gate  # statement coverage >= 99% on ./... (scripts/coverage-gate.sh)
$ make examples    # build, generate and test both examples
$ make site        # build this documentation site with hugo
```

`make test` runs two passes because the race detector is built on cgo, and the
toolchain rejects `-race` with `CGO_ENABLED=0`. The first pass proves the no-cgo
property; the second finds data races. `make verify` runs build, lint, test,
coverage and examples in one shot.

## Repository layout

| Path | Contents |
|---|---|
| [`cppgo.go`](https://github.com/Manu343726/cppgo/blob/main/cppgo.go), [`loadfs.go`](https://github.com/Manu343726/cppgo/blob/main/loadfs.go), [`object.go`](https://github.com/Manu343726/cppgo/blob/main/object.go), [`reentrant.go`](https://github.com/Manu343726/cppgo/blob/main/reentrant.go) | the runtime package the generated code imports |
| [`internal/asm`](https://github.com/Manu343726/cppgo/blob/main/internal/asm) | the NOSPLIT call trampolines and the stack probe |
| [`internal/callback`](https://github.com/Manu343726/cppgo/blob/main/internal/callback) | C++→Go callbacks through `runtime.cgocallback` |
| [`internal/cxx`](https://github.com/Manu343726/cppgo/blob/main/internal/cxx) | libclang, reached through `purego` |
| [`internal/gen`](https://github.com/Manu343726/cppgo/blob/main/internal/gen) | selection, carrier routing, the Go/asm/shim emitters, the shim compile |
| [`internal/errormap`](https://github.com/Manu343726/cppgo/blob/main/internal/errormap) | the error projection and its configuration (not consumed by the generator yet) |
| [`internal/shim`](https://github.com/Manu343726/cppgo/blob/main/internal/shim) | the construct matrix, measured against a real compiler |
| [`cmd/cppgo`](https://github.com/Manu343726/cppgo/blob/main/cmd/cppgo) | the command line |
| [`examples/`](https://github.com/Manu343726/cppgo/blob/main/examples) | `mathx` and `stats`, each with its own build and tests |
| [`site/`](https://github.com/Manu343726/cppgo/blob/main/site) | the Hugo sources of the documentation site |
| [`docs/adr/`](https://github.com/Manu343726/cppgo/blob/main/docs/adr) | the decision records |

## Documentation

The manual is published at <https://manu343726.github.io/cppgo/> and built from
[`site/`](https://github.com/Manu343726/cppgo/blob/main/site) with Hugo 0.169.
It follows four sections: tutorials, how-to guides, reference and explanation.

- [`DESIGN.md`](https://github.com/Manu343726/cppgo/blob/main/DESIGN.md) is the
  design contract the code is held to.
- [`docs/adr/`](https://github.com/Manu343726/cppgo/blob/main/docs/adr) holds one
  decision record per decision, never edited after acceptance.
- [`examples/`](https://github.com/Manu343726/cppgo/blob/main/examples) is the
  executable half of the tutorial.

Where the contract and the site disagree about what exists, the code and its
tests decide: the site describes the build in the tree.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). In short: `make verify` must pass, no
change may add cgo to the runtime, and a generated file is never edited by hand.

## Licence

MIT. See [`LICENSE`](https://github.com/Manu343726/cppgo/blob/main/LICENSE).
