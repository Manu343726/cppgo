+++
title = "cppgo"
description = "Call C++ from Go with no cgo: libclang resolves the declaration, cppgo generates the trampoline, and the call goes straight into the C++ ABI."
weight = 0
+++

**cppgo calls C++ from Go with no cgo at build time and no `import "C"`.** It
generates one `NOSPLIT` assembly trampoline per bound function, so a goroutine
enters the System V C ABI directly instead of going through the runtime's
`cgocall` path.

The default is the **direct C++ ABI mode**. You write a Go declaration with a
directive, cppgo asks libclang for the declaration's real type and mangled
symbol, validates that the call can work, and emits the trampoline that passes
`this` in `RDI`. There is no `extern "C"` layer to write, and none to keep in
sync. The old shim mode still exists as an opt-in escape hatch
(`//cppgo:mode shim`) for the signatures the direct ABI cannot express.

## How the call is put together

```
your Go code
  └── generated wrapper        idiomatic types: []float64, string, error
        └── generated asm      NOSPLIT, one per signature, no runtime involvement
              └── your C++     the symbol libclang reported, called directly
```

The trampoline reads its arguments from the Go frame, saves `SP` in `R13`,
aligns `SP` to 16 bytes because SysV wants `RSP` 16-aligned *before* the `CALL`,
calls through `R12`, restores `SP`, and only then stores the result — the result
slot is addressed relative to `SP`. Because the runtime never treats an assembly
function as asynchronously preemptible (`isAsyncSafePoint` returns false for any
`FuncFlagAsm` frame) and a foreign stack pointer is never inside `g.stack`, the
garbage collector simply skips the goroutine while the call is in flight.

## Measured cost

The table below is the short version; the [benchmarks
page]({{< ref "project/benchmarks.md" >}}) explains how to reproduce it and
carries the clock caveat.

## What it does not do

- Linux/amd64 only. The package builds elsewhere and panics with a clear message.
- No callbacks from C++ into Go in v1.
- C++ exceptions must not cross the boundary: in shim mode you catch them in
  your own entry point, and the direct ABI mode has no entry point to catch
  them, so a throw is undefined behaviour.
- A class returned by value is rejected in direct-ABI v1; use the shim mode.
- A binary that loads a shared library at run time is dynamically linked by
  definition. The promise is "no cgo, no C toolchain", not "a static file".

Start with the [manual]({{< ref "manual/_index.md" >}}) if you want the whole
contract, the [quickstart]({{< ref "manual/quickstart.md" >}}) if you want a
working binding now, or the [tutorials]({{< ref "tutorials/_index.md" >}}) if you
prefer to learn by building one.
