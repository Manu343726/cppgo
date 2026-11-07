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

And unlike cgo:

- `CGO_ENABLED=0` builds and runs. No `import "C"` anywhere.
- No C toolchain needed to build the Go side, so cross-compilation works.
- (A binary that loads a shared library at run time is dynamically linked by
  definition. The promise is "no cgo, no C toolchain", not "a static file".)
- The C++ library is loaded at runtime with `dlopen`, so it can be swapped,
  versioned, or shipped separately.

## How it works

```
your Go code
  └── generated wrapper        idiomatic types: []float64, string, error
        └── generated asm      NOSPLIT, one per signature, no runtime involvement
              └── your C++     an extern "C" shim we generate the header for
```

The trampoline is deliberately small:

```asm
TEXT ·Dot(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	a+8(FP), DI          // pointer
	MOVQ	n+16(FP), SI         // length
	MOVQ	SP, R13
	ANDQ	$~15, SP             // SysV wants RSP 16-aligned before the CALL
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+24(FP)
	RET
```

No `entersyscall`, no `P` handoff, no `g0` switch, no safepoint bookkeeping.
Because the runtime never treats an assembly function as preemptible
(`isAsyncSafePoint` returns false for any `FuncFlagAsm` frame), and a foreign
stack pointer is never inside `g.stack`, the GC simply skips the goroutine while
the call is in flight.

## What you give up

Being honest about the trade-offs is the point of this project, so:

- **The `P` is not released during the call.** A foreign call in flight delays a
  GC cycle by up to its duration (measured: a 2 ms call delayed the worst GC
  cycle by 1.1 ms, versus 2.4 ms for a spinning Go goroutine). This is the right
  trade for short, compute-bound calls and the wrong one for calls that block.
  For long or blocking work, move the C++ to a separate process.
- **No callbacks from C++ into Go.** Not supported in v1. Batch instead.
- **The C++ frame lives on the goroutine stack.** `cppgo` reserves
  `StackReserve` (32 KiB) of headroom before each call, so shims must stay well
  inside that.
- **Linux/amd64 only for now.** The package builds everywhere else and panics
  with a clear message. arm64 is next.
- **No exceptions across the boundary.** The generated shim catches
  `std::exception` and `...` and turns them into Go errors.

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
