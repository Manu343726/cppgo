# cppgo examples

Two complete, runnable bindings. Not fixtures: each one is a package you can
`cd` into, build, test and read, with the generated files checked in so the shape
of what cppgo produces is visible before you run anything.

Both are their own Go modules (each with a `go.mod` and a
`replace github.com/Manu343726/cppgo => ../..`), so the repository's own
`go test ./...` and its 99% coverage gate do not pick them up, while they still
build against the code next door rather than a published copy.

| example | what it is | command |
|---|---|---|
| [mathx](mathx/) | the direct ABI mode on one small C++17 class: constructor, destructor, `const` methods, a slice parameter, a static member — and the inline member the generator refuses on purpose | `cd examples/mathx && make` |
| [stats](stats/) | arithmetic over a million-element `[]float64`: `(pointer, length)` in both directions, a pointer-identity check that proves nothing is copied, a status turned into a Go error, and a benchmark against pure Go | `cd examples/stats && make && make bench` |

## Prerequisites

- Go 1.25 or newer.
- A C++17 compiler (`g++` by default; `make CXX=clang++` works).
- libclang, because `cppgo gen` parses the C++ headers with it:
  `sudo apt install clang libclang-dev`, `sudo pacman -S clang`, or the
  equivalent.
- Linux/amd64, which is what the trampolines are for today.

No `sudo`, no cgo, no `import "C"`: the examples compile with `CGO_ENABLED=0`
and load the C++ at run time with `dlopen`.

Each Makefile builds its own `cppgo` binary from `../../cmd/cppgo` into its
`build/` directory, so what runs is the generator in this repository, not
whatever happens to be on `PATH`.

## What to look at first

```console
$ cd examples/mathx && make
```

The interesting output is not the test result, it is the generator's report — one
line per binding, with the mangled symbol it resolved and which tier answered:

```
mathx::Vector::Vector              mangled _ZN5mathx6VectorC1Edd                          abi from compile_commands.json; note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::dot                 mangled _ZNK5mathx6Vector3dotEPKdm                     abi from compile_commands.json
mathx::Vector::~Vector             mangled _ZN5mathx6VectorD1Ev                           abi from compile_commands.json
```

Then open [`examples/mathx/zz_cppgo_amd64.s`](mathx/zz_cppgo_amd64.s): ten
instructions per function, `this` in `RDI`, a slice as `RSI`/`RDX`, the double
back in `XMM0`.

## Measured numbers

`cd examples/stats && make bench` — the median of three runs, with boost disabled
and the CPU pinned to **964.8 MHz**. The clock is pinned, so the absolute
nanoseconds are roughly 3.3x what a boosting core would give; the ratios and the
cycle counts are the numbers that travel.

| benchmark | cppgo | pure Go | ratio |
|---|---|---|---|
| `Mean`, 8 elements | 44 ns | 21 ns | 2.1x slower |
| `Mean`, 1 048 576 | 3.40 ms (3.1 cy/elem) | 3.48 ms | 0.98x |
| `Stddev`, 1 048 576 | 6.87 ms (6.3 cy/elem) | 7.14 ms | 0.96x |
| `Histogram`, 1 048 576 | 11.9 ms (10.9 cy/elem) | 12.1 ms | 0.98x |
| `CountAbove`, 1 048 576 | 1.63 ms (1.5 cy/elem) | 2.34 ms | 0.69x |
| the call itself (`DataPointer`) | 35.0 ns | 2.2 ns | 15.7x |

A repeat run while other builds were running in the background read 51–69 ns for
the 8-element row and 37–56 ns for the call: absolute values move with the load,
the ratios and the fixed ~35 ns crossing do not.

The two rows that matter are the first and the second: **35 ns of boundary is
~80% of an 8-element call and 0.001% of a million-element one.** The crossing is
a constant; the data is not. That is the whole argument for binding the loop, not
the accessor, and [`examples/stats/README.md`](stats/README.md) works through the
rest of the table (including why `CountAbove` is 1.45x faster on the C++ side:
g++ 16 vectorises the loop at `-O2` and the Go compiler does not).

## How they relate to the documentation site

The site's [examples](https://manu343726.github.io/cppgo/examples/) pages are
written against these directories, and the tutorials go deeper on the pieces:

- [Your first binding](https://manu343726.github.io/cppgo/tutorials/first-binding/) — the shim mode, start to finish.
- [Stateful objects](https://manu343726.github.io/cppgo/tutorials/stateful-objects/) — handles, lifetimes, double close.
- [Bulk data](https://manu343726.github.io/cppgo/tutorials/bulk-data/) — slices as `(pointer, length)`, and when not to loop.
- [When the generator refuses](https://manu343726.github.io/cppgo/tutorials/when-generation-refuses/) — the inline member `mathx` demonstrates with `make inline-demo`.

## In CI

`make examples` at the repository root runs `scripts/build-examples.sh`, which
runs each example's `make` — so both are built, generated, tested and vetted on
every CI run, without contributing a line to the coverage profile (they are
separate modules).

## Adding another example

An example is a directory here with a `Makefile` whose default target builds the
C++ shared library, runs `cppgo gen`, and runs `go test`. `scripts/build-examples.sh`
picks it up by that Makefile alone. Keep the generated `zz_cppgo*` files checked
in and give the reader a `make check` target: the point of a checked-in generated
file is that it can be proved current.
