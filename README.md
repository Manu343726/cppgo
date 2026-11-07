# cppgo

Call C++ from Go **without cgo**, without `import "C"`, and without paying the
cgo tax on every call.

`cppgo` generates a NOSPLIT assembly trampoline per bound function, so a Go
goroutine enters the System V C ABI directly instead of going through the
runtime's `cgocall` path (which hands off the P, switches to `g0`, and marks the
goroutine as being in a syscall).

```go
//cppgo:library libmathx
//cppgo:header include/mathx/vector.hpp

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)

//cppgo:method norm
func (v Vector) Norm() float64

//cppgo:method dot
func (v Vector) Dot(ys []float64) float64
```

```console
$ cppgo gen -dir ./mathx
```

That is the whole interface — and there is no C layer to write. `cppgo` reads
your headers through libclang (with the flags clangd would use, from
`compile_commands.json` or `.clangd`), resolves `mathx::Vector::norm` to its
mangled symbol `_ZNK5mathx6Vector4normEv`, and emits the trampoline that passes
`this` in `RDI`. If the binding is impossible — the method is defined inline and
has no symbol, or it is virtual, or it takes a `std::string` by value — `cppgo
gen` fails the build and says why, instead of emitting something that segfaults
in production.

## Why

Measured with the CPU pinned to **964 MHz** and boost disabled, so the
nanoseconds are inflated by the clock: trust the cycle counts and the ratio.

| path | ns/op | ~cycles | |
|---|---|---|---|
| plain Go indirect call | 2.1 | 2 | baseline |
| **cppgo trampoline** | **13.9** | **13** | **7.5x cheaper than cgo** |
| cgo (same function, same dlopen'd symbol) | 105 | 101 | |

A call is not free — 13 cycles is not 2 — but it is close enough to a normal Go
call that the boundary stops dictating your architecture. You no longer have to
batch 10 000 rows into one call just to amortise the crossing.

And unlike cgo, there is no `import "C"` in the tree:

- `CGO_ENABLED=0` builds and runs, and no C toolchain is needed for the Go side,
  so cross-compilation works.
- (A binary that loads a shared library at run time is dynamically linked by
  definition. The promise is "no cgo, no C toolchain", not "a static file".)
- The C++ library is loaded at runtime with `dlopen`, so it can be swapped,
  versioned, or shipped separately.

## How it works

One Go declaration becomes a generated Go wrapper and one generated `NOSPLIT`
assembly trampoline. The wrapper converts between Go types (`[]float64`,
`string`, `error`) and the registers the C ABI expects; the trampoline switches
`SP` and calls the mangled C++ symbol directly. The boundary is about 13 cycles,
which is the table above.

The trampoline itself, what the collector sees while a foreign frame sits on the
goroutine's stack, and how the library is loaded are in the [architecture
page](https://manu343726.github.io/cppgo/project/architecture/).

## What you give up

Being honest about the trade-offs is the point of this project, so:

- **The `P` is not released during the call.** A foreign call in flight delays a
  GC cycle by up to its duration. This is the right trade for short,
  compute-bound calls and the wrong one for calls that block — for long or
  blocking work, move the C++ to a separate process. The measurement is on the
  [benchmarks page](https://manu343726.github.io/cppgo/project/benchmarks/).
- **No callbacks from C++ into Go.** Not supported in v1. Batch instead.
- **The C++ frame lives on the goroutine stack.** `cppgo` reserves
  `StackReserve` (32 KiB) of headroom before each call.
- **Linux/amd64 only for now.** The package builds everywhere else and panics
  with a clear message. arm64 is next.
- **No exceptions across the boundary.** In shim mode the generated entry point
  catches `std::exception` and `...` and turns them into Go errors; in the
  direct ABI mode there is nothing to catch, so a throw is undefined behaviour.

## Requirements

- Go 1.25+
- Linux/amd64
- A C++17 compiler, to build *your* library (not to build the Go side)

## Install

```console
$ go install github.com/Manu343726/cppgo/cmd/cppgo@latest
```

## Status

Early but complete end to end: runtime, generator, examples, benchmarks, and a
99% coverage gate in CI. See the [documentation site](https://manu343726.github.io/cppgo/)
and the [API reference](https://pkg.go.dev/github.com/Manu343726/cppgo).

## License

MIT — see [LICENSE](LICENSE).
