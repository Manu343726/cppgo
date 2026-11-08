# Examples

Two small C++ libraries bound with `cppgo`, each owning its build, plus the tests
that prove the generated packages work. They are the executable half of the
[tutorial](https://manu343726.github.io/cppgo/tutorials/first-binding/): every
command on that page is one of these `Makefile` targets.

| Example | What it shows |
|---|---|
| [`mathx`](mathx/) | one C++17 class end to end: a constructor, a destructor, an accessor, methods that can fail, a method that cannot, a struct passed by value, and a function taking two slices |
| [`stats`](stats/) | one header with five functions: four that cannot fail and one that can, all taking their data as `(pointer, length)` so nothing is copied |

## Requirements

| Tool | Version |
|---|---|
| Go | 1.25 or newer (each example's `go.mod` declares `go 1.25.0`) |
| A C++ compiler | C++17; the Makefiles default to `g++` (`CXX` overrides it) |
| libclang | 20 or newer, for `cppgo gen` and `cppgo check` |
| `make` | any |

libclang is needed to generate, not to run. A program built against these
packages needs the two C++ files and no C++ toolchain at run time.

## Commands

```console
$ make -C examples/mathx          # build the library, generate, test, vet
$ make -C examples/stats
$ make examples                   # the repository's target: runs both
```

Each example exposes the same targets:

| Target | What it does |
|---|---|
| `make` | `gen`, then `test`, then `vet` |
| `make lib` | builds the user's shared object under `build/`, with the project's own flags and soname |
| `make gen` | runs `cppgo gen`: writes the Go API and the C++ side, and builds the C++ side |
| `make test` | `go test`, once with `CGO_ENABLED=0` and once with `-race` |
| `make check` | `cppgo check`: proves the committed generated sources are current |
| `make clean` | removes `build/` and the compiled C++ side |

## What is committed, and what is not

Each example commits the **generated sources** (`zz_cppgo.gen.go`,
`zz_cppgo_amd64.s`, `cppgo_shim/`) because an example is teaching material: the
command in the file's banner reproduces them, and a reader can see what the
generator wrote without running it. The compiled C++ library and `build/` are
ignored.

## Documentation

- [Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/) — the tutorial that uses `mathx`.
- [Generated API](https://manu343726.github.io/cppgo/reference/generated-api/) — the shape of the packages these examples contain.
- [Architecture](https://manu343726.github.io/cppgo/explanation/architecture/) — how the generated files work together.
- [Performance](https://manu343726.github.io/cppgo/explanation/benchmarks/) — what a call costs.
