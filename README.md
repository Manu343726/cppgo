# cppgo

cppgo gives your Go program a native Go API for a C++ library. You point it at
the library's header and at the way your project already compiles it, and it
writes the Go package you call. Your C++ sources, your build and your library
stay as they are, and the Go side needs **no cgo at build time and no
`import "C"`**.

It is a generator (`cppgo gen`) plus a small Go runtime that the generated
package imports. libclang and a C++ compiler are needed to *generate*. The
program you ship needs neither.

**Status.** The generator, the runtime and the two examples are in the tree and
tested. The full error projection, the wider operators/inheritance work and the
OpenCV, ArrayFire and Eigen target slices are designed and not shipped; each is
marked where it appears in the documentation.

## Quick start

You need:

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.25 or newer (`go.mod`: `go 1.25.0`) | building the generator, the runtime and your program |
| libclang | **20 or newer** | `cppgo gen` and `cppgo check` only |
| A C++ compiler | C++17; the same family and standard library as your library | generating |
| `make` | any | the repository's targets |

Build the generator and run the example, which binds one small C++17 class. The
example builds its own `cppgo`, so `bin/cppgo` is the one you use on your own
project:

```console
$ make build                     # bin/cppgo, with CGO_ENABLED=0
$ make -C examples/mathx gen     # build libmathx.so, generate, build the C++ side
```

Generation reads the header with the include paths and language standard your
project uses (`compile_commands.json`, or a `.clangd` file):

```console
$ build/cppgo gen -header include/mathx/vector.hpp -package mathx \
                  -library build/libmathx.so -out .
```

It prints one line per function or method it bound. The package it wrote is
ordinary Go:

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
ok  	github.com/Manu343726/cppgo/examples/mathx	0.018s
ok  	github.com/Manu343726/cppgo/examples/mathx	1.034s   # -race
```

The full walkthrough is
[Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/).

## What the generated package looks like

The package mirrors the C++ API with Go types, and every C++ rule about failure
shows up as a Go signature you can read:

- a constructor returns `(*Type, error)`; `Close` releases the object. There is
  no finalizer, so closing is your job.
- a method that can fail returns `(T, error)`; a method that cannot fail returns
  its value with no error at all.
- a C++ record with a natural layout is a Go struct with the same fields.
- a free function, a static member and an overload are package functions.
- a Go slice stands for a `(pointer, length)` pair and is borrowed for the call,
  not copied.

The exact shapes, per declaration, are in
[Generated API](https://manu343726.github.io/cppgo/reference/generated-api/).

## Errors

**A call that can fail returns `(T, error)`; a call that cannot fail returns no
error.** Two C++ conventions are projected today:

- **exceptions** — the Go error is a `*cppgo.CallError` naming the C++
  declaration, wrapping a `*cppgo.NativeError` that carries the library's
  message. `errors.As` finds either one.
- **status codes** — mark a declaration with `-status` and its 32-bit return
  becomes an error: the Go signature returns only the error, and a non-zero value
  is the failure.

Designed and not shipped
([issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19)):
`std::error_code`, `std::expected`, `errno`, `std::variant`, a configured `bool`
plus out-parameters, and custom result types. Until then, a function returning
`std::error_code` is refused at generation time with a message that says what to
do.

[Report failures from C++ to Go](https://manu343726.github.io/cppgo/how-to/errors/)
has the error types, the `errors.As` examples and the designed table.

## Deployment

cppgo builds a small C++ library for you at generation time, and your program
loads it together with your own library at run time. For deployment:

- keep the two C++ files in one directory, and open the generated one by a path
  that contains a slash; or
- put them on the loader's search path; or
- name your library explicitly with `CppgoLibraryPath` or the
  `CPPGO_LIBRARY_PATH` environment variable.

`cppgo.OpenFS` lets the C++ files travel inside the Go binary instead: it embeds
them with `//go:embed`, extracts them to a private per-user cache and loads them
from there. It is implemented and tested in the runtime; a *generated* package
does not call it yet, so embedding is currently a runtime API.

[Ship a program that uses a binding](https://manu343726.github.io/cppgo/how-to/deploy/)
has the steps and the failure messages;
[Embed the C++ side in the Go binary](https://manu343726.github.io/cppgo/how-to/embed/)
covers embedding and its limits.

## Target libraries

The design is exercised against three libraries. **None of the three is
implemented yet**; they are the acceptance tests the design describes, each with
a build tag and a skip when the library is absent.

| Target | What it will exercise |
|---|---|
| **Eigen** | a header-only, template-heavy library: the two-phase template flow and fixed-size matrices passed by value |
| **OpenCV** | whole-header binding at scale: hundreds of overloads, `cv::Mat` as an opaque value, `std::string`/`std::vector` everywhere |
| **ArrayFire** | a C++ API over a C API: opaque handles, and an exception type that must arrive as a Go error |

## Limits

The full list, with the test that measures each one, is
[Limits](https://manu343726.github.io/cppgo/explanation/limits/). The ones that
shape day-to-day use:

- **enums, inheritance, operators, member templates, smart pointers and
  variadic functions are not supported yet.** Each is a generation error naming
  the declaration and a workaround.
- **a Go function passed to C++ takes at most six integer and eight
  floating-point arguments.**
- **`long double`, `__int128`, `void *` and `nullptr_t` have no Go equivalent.**
- **a macro-only API cannot be bound**: libclang sees a macro's expansion, not
  its declaration.
- **`char *` parameters do not bind in this build.** Use `std::string` or
  `std::string_view` at the C++ boundary.
- **the C++ library cppgo builds must match your library's compiler and standard
  library.** Nothing detects a mismatch for you.

## Build and test

```console
$ make build       # CGO_ENABLED=0 go build ./..., then every cmd/ binary into bin/
$ make test        # two passes: CGO_ENABLED=0 go test, then CGO_ENABLED=1 -race
$ make lint        # go vet, then golangci-lint when it is installed
$ make cover-gate  # statement coverage >= 99% on ./... (scripts/coverage-gate.sh)
$ make examples    # build, generate and test both examples
$ make site        # build this documentation site with hugo
```

`make test` runs two passes because the race detector needs cgo, and the
toolchain rejects `-race` with `CGO_ENABLED=0`. The first pass proves the no-cgo
property; the second finds data races. `make verify` runs build, lint, test,
coverage and examples in one shot.

## Documentation

The manual is published at <https://manu343726.github.io/cppgo/> and built from
[`site/`](https://github.com/Manu343726/cppgo/blob/main/site) with Hugo 0.169.
It follows four sections: tutorials, how-to guides, reference and explanation.
[Architecture](https://manu343726.github.io/cppgo/explanation/architecture/) is
where *how it works* is written down.

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
