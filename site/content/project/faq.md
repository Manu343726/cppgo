+++
title = "FAQ"
description = "Why not cgo, why not SWIG, why purego is used for libclang, why shared memory is not always the answer, and the three questions that come up after the first binding."
weight = 40
+++

## Why not cgo?

Because the cost is in the runtime path, not in the call. cgo is the supported,
battle-tested way to call C from Go, and if your bottleneck is not the boundary
you should use it. cppgo exists for the case where the boundary is the workload.

The measured difference, on the same machine and the same `dlopen`'d function:

| path | ns/op | ~cycles |
|---|---|---|
| cgo | 105 | 101 |
| cppgo trampoline | 13.9 | 13 |

(Same clock caveat as everywhere on this site: the CPU was pinned to 964 MHz, so
trust the cycles and the ratio, not the nanoseconds.) cgo pays for
`entersyscall`, the `P` handoff, the `g0` stack switch and the bookkeeping that
marks the goroutine as being in a syscall. cppgo keeps the `P` and calls from a
`NOSPLIT` assembly frame, so none of that happens.

The secondary reasons are build-shaped:

- **No `import "C"`, so no C toolchain to build the Go side.** Cross-compiling
  the Go binary no longer needs a C cross-compiler; only *running* it needs the
  `.so`, which you can ship separately.
- **`CGO_ENABLED=0` builds and tests the library.** The test suite proves it in
  a separate pass.
- **The symbols are resolved at run time**, so the same binary can load a
  different build of the library.

The honest costs: cgo is more mature, it supports callbacks, and it can pass
arbitrary C types because the compiler writes the glue. cppgo's direct mode
rejects what it cannot lay out, and the `P` is not released during a call.

## Why not SWIG?

SWIG is not evaluated in this project's design contract, so read this as a
statement of scope rather than a benchmark: **we have not measured SWIG, and no
number on this site compares against it.**

The architectural difference is what cppgo is built around. A general binding
generator is built to target many languages through a common interface, so its
generated glue typically goes through a stable C ABI and a language-specific
runtime. cppgo is Go-only and generates, per signature, a `NOSPLIT` assembly
trampoline that enters the C++ ABI directly: `this` in `RDI`, floats in `XMM`,
no shim function, no generic dispatch. That is only possible because the target
language and the target ABI are fixed.

The other half is validation. cppgo asks libclang whether a binding is *possible*
— out-of-line body, not virtual, no `std::` by value — and fails the build when
it is not. A generator that accepts every declaration has to defer those
failures to run time.

If you need many languages, or you need all of C++ including templates and
callbacks, use a general generator. If you need fast Go calls into a C++ library
you can shape, cppgo is aimed at you.

## Why does cppgo depend on purego — isn't an FFI depending on an FFI ironic?

It is a fair joke, and the decision is recorded rather than dodged.

cppgo uses purego for two things: `dlopen`/`dlsym` without cgo, and (inside
`internal/cxx`) the libclang calls that pass structs by value.

The decisive reason is not "purego is popular". It is that **a dynamically
linked Go binary is not a normal C program**: the Go runtime installs its own
`%fs` thread-control block, so libc code that reaches for thread-local storage —
`errno`, `dlerror`'s per-thread result, `pthread_getspecific` — runs against a
TCB that glibc never initialised. purego solves that in `internal/fakecgo`, which
its non-cgo path imports on purpose. Writing our own loader would have meant
owning fakecgo too, which is a larger and less forgiving job than the loader
itself. A dynamic loader is also a maintenance liability: libc symbol versioning
changes quietly, and purego has been doing this in production for years on four
platforms.

The differentiator is not the loader and not the calling convention. It is the
C++ layer: mangled symbols, implicit `this`, `const` methods, constructors, and
knowing at generation time whether a binding is even possible. That is where the
effort goes.

purego is also used as a **call** path for libclang, because `CXString`, `CXType`,
`CXCursor` and `CXSourceLocation` are passed and returned by value, including
memory-class returns through a hidden pointer. Those are exactly the signatures
the generated trampoline refuses for user bindings; using a library that already
implements the aggregate rules is cheaper than writing a second ABI.

## Why not shared memory always?

Because a `[]float64` already *is* shared memory.

When a slice crosses, cppgo passes a pointer to Go's own backing array and a
length. There is no copy, no serialisation, no second buffer: C++ reads and
writes the same bytes Go sees. The [stats example]({{< ref
"examples/stats.md" >}}) proves it with a pointer-identity check. So the question
is not "shared memory or not" for bulk data — it already is shared memory.

What "always use shared memory" would add is a **protocol**: a mapping to create,
a lifetime to negotiate, a way to signal "the buffer is ready" and "I am done
with it", and a failure mode when one side dies. For a call that takes 13 cycles,
that protocol costs orders of magnitude more than the call. For a long-running or
blocking operation it is the right shape — which is why the design says: for
long or blocking work, move the C++ to a **separate process**, where shared
memory (or a pipe, or a socket) is how you talk to it.

The rule of thumb:

- **short, compute-bound call** — call it. The boundary is ~13 cycles.
- **large data, short call** — pass the slice. No copy, no protocol.
- **long or blocking call** — another process, with a real transport.
- **per-element calls in a loop** — bind the loop instead; see [bulk
  data]({{< ref "tutorials/bulk-data.md" >}}).

## Why is my `(T, error)` always `nil`?

Because a direct C++ call has no status channel. A `double` result and an error
cannot both come back in one return register, and there is no shim to store a
message. The generator says so in the report and in the generated file:

> the trailing error is always nil: a direct C++ call has no status channel
> (return an int32 status and declare only error to get one)

Two ways to get a real error: have the C++ function return an integer status and
declare only `error` (the integer becomes the status), or use shim mode, where
the generated signature is `int32_t f(..., out)` and a non-zero status becomes a
Go error. The convention is in [error handling]({{< ref "manual/errors.md" >}}).

## Why does the generator need the `.cpp` files? The header declares everything.

The header declares, but it does not say whether a member has an out-of-line
definition. A member defined inside the class body is implicitly inline, emits no
dynamic symbol, and cannot be bound — and the header alone cannot distinguish it
from one defined in a `.cpp`. So the generator parses the header *and* the
translation units that include it, then merges the sightings by mangled name. A
declaration seen only in a header is a binding that would fail at load time, and
cppgo refuses it at generation time.

That is also why a header-only library is a problem: if there is no out-of-line
definition anywhere, there is no symbol to find.

## Why no finalizer?

A finalizer runs at an unpredictable time, on an arbitrary goroutine, possibly
while the library is closing. Calling a destructor from there is a
use-after-free waiting to happen — the object's library may already be unloaded,
or another goroutine may be mid-call. The design chooses explicitness: every
constructor has a destructor, and Go calls it. `defer v.Destroy()` is the idiom.

## Can I bind a C library?

The shim mode is `extern "C"`, so yes, mechanically: declare
`//cppgo:external <symbol>` and implement the entry points in C or C++. But the
project is aimed at C++: the direct mode's value is the mangled-name, `this` and
validation work, none of which a C library needs. For a pure C library, cgo or
purego directly is a better fit.

## Why one library per package?

`//cppgo:library` is package-level and singular: the generated `cppgoEnsure`
opens one library and resolves that package's symbols in it. If you need two
libraries, use two binding packages — which also keeps each package's
`CppgoLibraryPath` meaningful and its symbol namespace small.

Back to the [manual]({{< ref "manual/_index.md" >}}).
