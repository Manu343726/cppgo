# cppgo examples

Two complete, runnable bindings, written header-first against `cppgo gen`. They are real packages. Each is a package you can `cd` into, build, test and read.

```console
$ cd examples/mathx && make
$ cd examples/stats  && make && make bench
```

Both are their own Go modules (each with a `go.mod` and a `replace github.com/Manu343726/cppgo => ../..`). The repository's own `go test ./...` and its coverage gate do not pick them up, while they still build against the code next door rather than a published copy.

| example | what it is | command |
|---|---|---|
| [mathx](mathx/) | one small C++17 class: a constructor, a destructor, a `const` method defined inline, a slice parameter, a `noexcept` method the generator calls directly, a method that throws, a POD by value and a BLAS-shaped free function | `cd examples/mathx && make` |
| [stats](stats/) | arithmetic over a million-element `[]float64`: `(pointer, length)` in both directions, a pointer-identity check that proves nothing is copied, an exception turned into a Go error, and a benchmark against pure Go | `cd examples/stats && make && make bench` |

## What v4 means for these examples

The C++ header and source are the source of truth. Neither example carries a cppgo annotation. No `//cppgo:` directive, no `import "C"`, no
hand-written declaration. The flow has three artefacts:

1. **the user's library** — `include/…` and `src/…`, compiled by the example's
   own Makefile with its own flags and a soname. cppgo never writes into it and
   never asks it to include anything.
2. **the cppgo shim** — `cppgo gen` reads the header through libclang and
   writes `cppgo_shim/zz_cppgo_shim.{h,cpp}`. It compiles them itself into
   `libcppgo_<package>.so`, linked against the user's library. A thunk exists
   for every declaration the direct ABI cannot call, and it is where `try`,
   `catch`, `std::string` and by-value records live.
3. **the generated Go package** — `zz_cppgo.gen.go` and `zz_cppgo_amd64.s`:
   the API a caller sees, plus one NOSPLIT trampoline per binding. It loads the shim (which pulls in the user's library through its `DT_NEEDED` entry) with `dlopen`. There is no cgo at build time.

The report `cppgo gen` prints names the **carrier** of every line. `direct`
means the trampoline calls the mangled C++ symbol in the user's library. `thunk`
means it calls a shim function, for a reason the report states.

## Prerequisites

- Go 1.25 or newer.
- A C++17 compiler (`g++` by default. `make CXX=clang++` works).
- libclang, because `cppgo gen` parses the headers with it:
  `sudo apt install clang libclang-dev`, `sudo pacman -S clang`, or the
  equivalent.
- Linux/amd64, which is what the trampolines target today.

No `sudo`, no cgo, no `import "C"`. Each Makefile builds its own `cppgo` from
`../../cmd/cppgo` into its `build/` directory, so what runs is the generator in
this repository, not whatever happens to be on `PATH`.

## What to look at first

```console
$ cd examples/mathx && make
```

The interesting output is the generator's report, one line per binding:

```
mathx::Vector::scale(double) noexcept          direct _ZN5mathx6Vector5scaleEd   noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::x() const                       thunk  cppgo_thunk_000…           not noexcept: an exception would unwind into Go
mathx::Vector::at(std::size_t) const           thunk  cppgo_thunk_79ae…          not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                  thunk  cppgo_thunk_a666…          a record passed or returned by value: the thunk copies it
```

`scale` is the one member called directly. It is `noexcept` and defined out of
line, so its mangled symbol is in the library and the Go signature has no
`error`. `x` is defined inside the class body, `at` may throw and `midpoint`
takes a record by value, so each of those is a shim call. Then open the
generated API, `zz_cppgo.gen.go`, and compare it with the header. The shape of
the Go package is derived from the C++ declarations, not declared beside them.

## The generated sources are committed

`zz_cppgo.gen.go`, `zz_cppgo_amd64.s` and `cppgo_shim/` are checked in on
purpose. An example is teaching material, and a reader should be able to see
what the generator produces without running it first.

- `make gen` reproduces them.
- `make check` runs `cppgo check` and fails if a committed file is stale.
- the compiled objects,  `build/` and `libcppgo_*.so`, are built, never
  committed. `examples/.gitignore` names them.

## In CI

`make examples` at the repository root runs `scripts/build-examples.sh`, which
runs each example's default target, so both are built, generated, tested (with
`CGO_ENABLED=0` and with the race detector) and vetted on every CI run, without
contributing a line to the coverage profile (they are separate modules).

## Adding another example

An example is a directory here with:

- a `Makefile` whose targets build the user's library (`-Wl,-soname,lib….so`),
  run `cppgo gen -header … -package … -library … -out .`, then test and vet.
- a `.clangd` (or a `compile_commands.json`) naming the project's real flags.
  The header is not parsed under an assumption.
- the generated sources committed, plus a `make check` target that proves they
  are current.

`scripts/build-examples.sh` picks the directory up by its Makefile alone. It
never compiles C++ itself. The example's own build is what produces the user's
library, and the generator's shim is what links against it.
