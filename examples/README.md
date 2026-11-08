# Examples

Two small C++ libraries bound with `cppgo`, each own its build, plus the tests
that prove the generated packages work. They are the executable half of the
[tutorial](https://manu343726.github.io/cppgo/tutorials/first-binding/): every
command on that page is one of these `Makefile` targets.

| Example | What it teaches |
|---|---|
| [`mathx`](mathx/) | one C++17 class end to end: a constructor, a destructor, an inline accessor, a throwing method, a `noexcept` method on the direct carrier, a POD passed by value, and a function taking two slices |
| [`stats`](stats/) | the carrier split in one header: four `noexcept` functions called directly and one throwing function called through the shim, all taking their data as `(pointer, length)` so nothing is copied |

## Requirements

| Tool | Version |
|---|---|
| Go | 1.25 or newer (each example's `go.mod` declares `go 1.25.0`) |
| A C++ compiler | C++17; the Makefiles default to `g++` (`CXX` overrides it) |
| libclang | 20 or newer, for `cppgo gen` and `cppgo check` |
| `make` | any |

libclang is needed to generate, not to run. A program built against these
packages needs the two shared objects and no C++ toolchain at run time.

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
| `make gen` | runs `cppgo gen`: writes the Go API, the assembly, the shim sources, and compiles the shim |
| `make test` | `go test`, once with `CGO_ENABLED=0` and once with `-race` |
| `make check` | `cppgo check`: proves the committed generated sources are current |
| `make clean` | removes `build/` and the compiled shim |

## What is committed, and what is not

Each example commits the **generated sources** (`zz_cppgo.gen.go`,
`zz_cppgo_amd64.s`, `cppgo_shim/`) because an example is teaching material: the
command in the file's banner reproduces them, and a reader can see what the
generator wrote without running it. The compiled shim (`libcppgo_<name>.so`) and
`build/` are ignored.

## Documentation

- [Bind your first C++ class](https://manu343726.github.io/cppgo/tutorials/first-binding/) — the tutorial that uses `mathx`.
- [Generated API](https://manu343726.github.io/cppgo/reference/generated-api/) — the shape of the packages these examples contain.
- [Architecture](https://manu343726.github.io/cppgo/explanation/architecture/) — what each generated file is for.
