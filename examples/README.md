# Examples

Six Go modules that call C++ with `cppgo`, each owning its build, plus the tests
that prove the generated packages work. `mathx` and `stats` are the executable
half of the [tutorial](https://manu343726.github.io/cppgo/tutorials/first-binding/);
`full` is the complete project, from a CMake-built library to a running Go
program, and has [a tutorial of its own](https://manu343726.github.io/cppgo/tutorials/full-project/).
`opencv`, `arrayfire` and `eigen` are slices of real, installed libraries: there
is no C++ source in those directories to build, and their tests assert values
against the library itself.

| Example | What it shows |
|---|---|
| [`mathx`](mathx/) | one C++17 class end to end: a constructor, a destructor, an accessor, methods that can fail, a method that cannot, a struct passed by value, and a function taking two slices |
| [`stats`](stats/) | one header with five functions: four that cannot fail and one that can, all taking their data as `(pointer, length)` so nothing is copied |
| [`full`](full/) | a complete project: a C++ library with its own CMake build (an opaque class, `std::string`, `std::vector`, a POD by value, a throw, a status code, a callback and templates), the generated Go, and a real `cmd/app` program with tests |
| [`opencv`](opencv/) | a slice of an installed OpenCV 4.10: the `core.hpp` enums, `cv::TermCriteria`, `cv::TickMeter`, and the version, tick, thread and CPU functions |
| [`arrayfire`](arrayfire/) | a slice of an installed ArrayFire 3.10 (CPU backend): the element-type and data-source enums, and the device functions that report the library's configuration |
| [`eigen`](eigen/) | a slice of an installed Eigen 3.4: three configuration enums and the thread functions; a header-only library, so the shim is the whole binary |

Each target module's README lists every declaration its slice binds and every one
it refuses, with the generator's message. `cv::Mat`, `af::array` and Eigen's
matrix types are not among them, so no image, array or matrix operation is
available through these modules.

## Requirements

| Tool | Version |
|---|---|
| Go | 1.25 or newer (each example's `go.mod` declares `go 1.25.0`) |
| A C++ compiler | C++17; `mathx` and `stats` default to `g++` (`CXX` overrides it), `full` lets CMake choose |
| CMake | 3.16 or newer, for `full` |
| libclang | 20 or newer, for `cppgo gen` and `cppgo check` |
| `make` | any |
| OpenCV, ArrayFire, Eigen | the version each target module is built against: OpenCV 4.10, ArrayFire 3.10 (CPU backend), Eigen 3.4 |

libclang is needed to generate, not to run. A program built against these
packages needs the two C++ files and no C++ toolchain at run time.

The target modules need their library present: OpenCV's core headers and
`libopencv_core.so`, ArrayFire's headers and `libaf.so`, Eigen's headers. Each
`Makefile` names the variable that moves the paths it found (`OPENCV_INCLUDE`
and `OPENCV_LIB`, `AF_INCLUDE` and `AF_LIB`, `EIGEN_INCLUDE`), and on a machine
without the library every target prints the missing path and exits zero.

## Commands

```console
$ make -C examples/mathx          # build the library, generate, test, vet
$ make -C examples/stats
$ make -C examples/full
$ make -C examples/opencv         # the same targets, against the installed library
$ make -C examples/arrayfire
$ make -C examples/eigen
$ make examples                   # the repository's target: runs all six
```

The examples share these targets; `full` adds `scan` and `run`, and in the
three target modules `lib` reports the installed library instead of building
one:

| Target | What it does |
|---|---|
| `make` | `gen` (and, for `full`, the CMake build and the Go program), then `test`, then `vet` |
| `make lib` | builds the user's shared object under `build/`, with the project's own flags and soname; in a target module, reports the library the binding links against |
| `make gen` | runs `cppgo gen`: writes the Go API and the C++ side, and builds the C++ side |
| `make scan` | `full` only: `cppgo build -scan`, which adds the program's template instantiations to the shim |
| `make test` | `go test`, once with `CGO_ENABLED=0` and once with `-race` |
| `make run` | `full` only: builds and starts the program |
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
- [Build a complete Go + C++ project](https://manu343726.github.io/cppgo/tutorials/full-project/) — the tutorial that uses `full`.
- [Generated API](https://manu343726.github.io/cppgo/reference/generated-api/) — the shape of the packages these examples contain.
- [Architecture](https://manu343726.github.io/cppgo/explanation/architecture/) — how the generated files work together.
- [Performance](https://manu343726.github.io/cppgo/explanation/benchmarks/) — what a call costs.
