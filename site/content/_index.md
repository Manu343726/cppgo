+++
title = "cppgo"
description = "Call C++ from Go with no cgo: libclang resolves the declaration, cppgo generates the trampoline, and the call goes straight into the C++ ABI."
weight = 0
+++

**cppgo calls C++ from Go with no cgo at build time and no `import "C"`.** You
write a Go declaration with a directive; cppgo asks libclang for the declaration's
real type and mangled symbol, validates that the call can work, and generates a Go
wrapper plus one `NOSPLIT` assembly trampoline per bound function.

The default is the **direct C++ ABI mode**: the trampoline calls the mangled C++
symbol with `this` in `RDI`, so there is no `extern "C"` layer to write and none
to keep in sync. The shim mode still exists as an opt-in escape hatch
(`//cppgo:mode shim`) for the signatures the direct ABI cannot express.

A call costs about 13 cycles, against cgo's 101 for the same `dlopen`'d function.
The [benchmarks page]({{< ref "project/benchmarks.md" >}}) has the table, how to
reproduce it, and the pinned-clock caveat both figures were taken under. What the
trampoline does, and why the `P` is never released, is [How it
works]({{< ref "project/architecture.md" >}}).

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
