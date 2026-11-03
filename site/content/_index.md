---
title: "cppgo"
description: "Call C++ from Go without cgo, without import \"C\", and without paying the cgo tax on every call."
weight: 1
---

**cppgo calls C++ from Go with no cgo at build time and no `import "C"`,** by
generating one `NOSPLIT` assembly trampoline per bound function that enters the
System V C ABI directly, instead of going through the runtime's `cgocall` path
(which hands off the `P`, switches to `g0`, and marks the goroutine as being in
a syscall).

You write a normal Go file with annotations. The generator writes the
idiomatic wrapper, the assembly, and the `extern "C"` header your C++ side
implements:

```go
//cppgo:library libmathx
//cppgo:external dot
func Dot(a, b []float64) (float64, error)
```

```console
$ cppgo gen -dir ./mathx
```

Because the C++ library is loaded at runtime with `dlopen`, it can be swapped,
versioned or shipped separately from your binary. Because nothing on the Go
side is compiled by a C compiler, `CGO_ENABLED=0` builds and runs, and
cross-compilation keeps working.

**What you get that cgo does not give you:**

- A static, single-file Go binary — no C toolchain needed to build the Go side.
- No `cgocall`: a call costs ~13 cycles instead of ~101.
- A library you can replace at deploy time, not just at build time.
- Idiomatic types at the boundary: `[]float64`, `string`, `error`.

**What it costs:** the `P` is not released during a call, so a foreign call in
flight delays a GC cycle by up to its duration. That is the right trade for
short, compute-bound calls and the wrong one for calls that block — see
[safety](/docs/safety/) and [limitations](/docs/limitations/).

New here? Start with [installation](/docs/installation/), then the
[quickstart](/docs/quickstart/). The [API reference](/docs/api/) is the
public surface; [how it works](/docs/how-it-works/) is the assembly.
