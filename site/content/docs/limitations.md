---
title: "Limitations and roadmap"
description: "What v1 deliberately does not do, which limits are permanent design decisions, and what is planned next."
weight: 90
---

cppgo v1 is a complete path — runtime, generator, examples, benchmarks — but it
is not a general-purpose FFI. This page is the honest list of what is out of
scope, split into things that are temporary and things that are the design.

## Platform

**Linux/amd64 only.** The trampoline is hand-written amd64 assembly, so the
generated code only exists for that target. On any other platform the package
still builds — and panics with a clear message rather than producing a
mysterious failure at the first call.

**Go 1.25 or newer** is required; `go.mod` pins the minimum.

**No Windows support in v1.** The dynamic loader path is `dlopen`/`dlsym`; there
is no `LoadLibrary` equivalent in the runtime.

## The boundary

**No callbacks from C++ into Go.** There is no reverse trampoline. A C++ API
that wants a Go function as a callback cannot be bound directly in v1; the
usual fix is to invert the control flow — have C++ return data and let Go
dispatch — or to do the work in a batch.

**No exceptions across the boundary.** They are not propagated, they are caught:
the generated shim converts `std::exception` and `...` into a non-zero status
plus a message. That is a deliberate guarantee, not a limitation to be lifted
later; unwinding into a Go frame is not survivable.

**Structs cross only by pointer**, and only as `//cppgo:repr(C)`. No by-value
structs, no unions, no bitfields, no C++ classes by value.

**No variadic bound functions.** No `...`, no generic type parameters, no maps,
channels, funcs or interfaces in a signature. The [type mapping](/docs/type-mapping/)
table is the complete set.

## Cost and behaviour

**The `P` is not released during a call.** A foreign call unavoidably delays a
GC cycle by up to its duration (2.0 ms call ⇒ 1.1 ms worst cycle). Short,
compute-bound calls are the intended use; long or blocking work belongs in
another process. This is the central trade-off of the project and it will not
change.

**32 KiB of stack reserve per call.** `cppgo.StackReserve` bounds how deep the
C++ side can go, because the foreign frames live in goroutine stack headroom
that cannot grow. Shims must stay well inside it.

**No cancellation, no timeouts, no context.** A call runs to completion. There
is no supported way to interrupt foreign code from Go.

**A `Handle` is not garbage collected.** C++ objects survive until you
explicitly destroy them; a leaked handle is a leaked object. See
[stateful objects](/tutorials/stateful-objects/).

## API surface

`cppgo` describes itself as early but complete end to end. The public API is
small and documented in the [API reference](/docs/api/), and the generated
files are build artifacts: after upgrading, re-run `cppgo gen` in each bound
package rather than patching the output. If you need stability across upgrades,
pin the module version in `go.mod` and upgrade deliberately.

## Roadmap

Items below are planned or under consideration — none of them exist in v1.

| item | status |
|---|---|
| **arm64 support** | Next. The package already builds on arm64 and panics with a clear message; the trampoline for AArch64 is the missing piece. |
| **Windows** | Not in v1. A `LoadLibrary`-based loader and a Windows ABI trampoline would both be needed. |
| **Callbacks from C++ into Go** | Not in v1. Would need a reverse trampoline and a defined answer for what a Go callback may do while the caller's stack is foreign. |

## What is not on the roadmap

- **Lifting the `P` trade-off.** Releasing the `P` is what cgo does, and it is
  what costs ~100 cycles. cppgo exists because that trade was worth refusing for
  short calls; there is no plan to reintroduce it.
- **A cgo-compatible build mode.** If you need cgo's semantics — callbacks,
  arbitrary struct passing, exceptions — use cgo.
- **C++ templates or name mangling.** Binding goes through `extern "C"` shims
  with fixed names, by design, so the ABI is stable and the symbol table is
  readable.

If you hit a limit that is not on this page, it is worth an issue: the point of
writing the limits down is that the interesting ones are known.
