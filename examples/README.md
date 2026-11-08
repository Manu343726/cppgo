# cppgo examples

Two complete, runnable bindings. Not fixtures. Each one is a package you can
`cd` into, build, test and read, with the generated files checked in so the shape
of what cppgo produces is visible before you run anything.

Both are their own Go modules (each with a `go.mod` and a
`replace github.com/Manu343726/cppgo => ../..`). The repository's own
`go test ./...` and its 99% coverage gate do not pick them up, while they still
build against the code next door rather than a published copy.

| example | what it is | command |
|---|---|---|
| [mathx](mathx/) | the direct ABI mode on one small C++17 class: constructor, destructor, `const` methods, a slice parameter, a static member — and the inline member the generator refuses on purpose | `cd examples/mathx && make` |
| [stats](stats/) | arithmetic over a million-element `[]float64`: `(pointer, length)` in both directions, a pointer-identity check that proves nothing is copied, a status turned into a Go error, and a benchmark against pure Go | `cd examples/stats && make && make bench` |

## Prerequisites

- Go 1.25 or newer.
- A C++17 compiler (`g++` by default. `make CXX=clang++` works).
- libclang, because `cppgo gen` parses the C++ headers with it:
  `sudo apt install clang libclang-dev`, `sudo pacman -S clang`, or the
  equivalent.
- Linux/amd64, which is what the trampolines are for today.

No `sudo`, no cgo, no `import "C"`: the examples compile with `CGO_ENABLED=0`
and load the C++ at run time with `dlopen`.

Each Makefile builds its own `cppgo` binary from `../../cmd/cppgo` into its
`build/` directory. What runs is the generator in this repository, taken from
the code next door.

## What to look at first

```console
$ cd examples/mathx && make
```

The interesting output is the generator's report, one
line per binding, with the mangled symbol it resolved and which tier answered:

```
mathx::Vector::Vector              mangled _ZN5mathx6VectorC1Edd                          abi from compile_commands.json; note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::dot                 mangled _ZNK5mathx6Vector3dotEPKdm                     abi from compile_commands.json
mathx::Vector::~Vector             mangled _ZN5mathx6VectorD1Ev                           abi from compile_commands.json
```

Then open [`examples/mathx/zz_cppgo_amd64.s`](mathx/zz_cppgo_amd64.s): ten
instructions per function, the arguments loaded into registers, the call, the
result copied back. [Register
assignment](https://manu343726.github.io/cppgo/manual/calling-convention/) says
which register holds what.

## Measured numbers

`cd examples/stats && make bench` compares each function against the pure-Go
equivalent over the same memory, at a pinned 964.8 MHz. The table, the
run-to-run spread and the clock caveat are in
[`examples/stats/README.md`](stats/README.md).

What the table argues, in two numbers: **35 ns for the call itself** is ~80% of
an 8-element `Mean` and 0.001% of a million-element one, and the million-element
arms run at the same cycles per element in both languages. The crossing is a
constant. The data is the variable. That is the argument for binding the loop. The one row where the languages differ is `CountAbove`, 1.45x faster on
the C++ side because g++ 16 vectorises the loop at `-O2` and the Go compiler does
while the Go compiler leaves the loop scalar. That is a compiler difference.

## How they relate to the documentation site

The site's [examples](https://manu343726.github.io/cppgo/examples/) pages are
written against these directories, and the tutorials go deeper on the pieces:

- [Your first binding](https://manu343726.github.io/cppgo/tutorials/first-binding/), one free function, from header to running program.
- [Stateful objects](https://manu343726.github.io/cppgo/tutorials/stateful-objects/), handles, lifetimes, double close.
- [Bulk data](https://manu343726.github.io/cppgo/tutorials/bulk-data/), slices as `(pointer, length)`, and when not to loop.
- [When the generator refuses](https://manu343726.github.io/cppgo/tutorials/when-generation-refuses/), the inline member `mathx` demonstrates with `make inline-demo`.

## In CI

`make examples` at the repository root runs `scripts/build-examples.sh`, which
runs each example's `make`. Both are built, generated, tested and vetted on
every CI run, without contributing a line to the coverage profile (they are
separate modules).

## Adding another example

An example is a directory here with a `Makefile` whose default target builds the
C++ shared library, runs `cppgo gen`, and runs `go test`. `scripts/build-examples.sh`
picks it up by that Makefile alone. Keep the generated `zz_cppgo*` files checked
in and give the reader a `make check` target. The point of a checked-in generated
file is that it can be proved current.
