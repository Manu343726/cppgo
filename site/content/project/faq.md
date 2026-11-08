+++
title = "FAQ"
description = "Why cppgo exists beside cgo, why SWIG is out of scope, why purego is a dependency, how a failure reaches Go, and the questions that come up after the first binding."
weight = 40
+++

**Audience:** you have a specific objection or a specific question about the
design.

## Why cgo costs more

Because the cost sits in the runtime path. cgo is the supported, battle-tested
way to call C from Go. Use it when the boundary is not your bottleneck. cppgo
exists for the case where the boundary is the workload.

The measured difference for the same `dlopen`'d function is **13 cycles against
101**: cgo pays for `entersyscall`, the `P` handoff, the `g0` stack switch and
the bookkeeping that marks the goroutine as being in a syscall. cppgo pays for
none of it. The [benchmarks page]({{< ref "project/benchmarks.md" >}}) has the
numbers, the pinned clock they were taken at, and the GC cost the shortcut buys.

The secondary reasons are build-shaped. The Go side needs no C toolchain, and cross-compiling needs no C cross-compiler. The symbols
are resolved at run time, so the same binary can load a different build.
[Installation]({{< ref "manual/installation.md" >}}) covers what that buys.

The honest costs are these. cgo is more mature, it supports callbacks, and it can pass
arbitrary C types because the compiler writes the glue. cppgo routes each
declaration to a carrier it can prove, and the `P` stays with the goroutine
during a direct call.

## Why SWIG is out of scope

SWIG has no evaluation in this project's design contract, so read this as a
statement of scope. **SWIG has not been measured here, and no number on this
site compares against it.**

The architectural difference is what cppgo is built around. A general binding
generator targets many languages through a common interface. Its generated glue
typically goes through a stable C ABI and a language-specific runtime. cppgo is
Go-only and generates, per signature, a `NOSPLIT` assembly trampoline that enters
the C++ ABI directly. That is only possible because the target language and the
target ABI are fixed.

The other half is the division of labour. cppgo asks libclang for the real
declaration and routes it to the direct carrier or to a generated C++ thunk.
It then compiles the shim itself. The constructs the register convention cannot express still have a C++ frame around them. A generator that accepts
every declaration without a C++ counterpart has to defer those failures to run
time.

If you need many languages, use a general generator. If you need fast Go calls
into a C++ library and can accept the ABI coupling, cppgo is aimed at you.

## Why purego is a dependency

It is a fair joke that an FFI depends on another FFI. The mechanism is in [How it works]({{< ref "project/architecture.md" >}}). This is the reason.

purego is used twice. As the loader (`dlopen`/`dlsym` with no cgo) and, inside
`internal/cxx`, as the call path for libclang's by-value structs.

The loader stays out of this project because a Go binary that calls into libc
has already left the world where thread-local storage is glibc-initialised. The
Go runtime installs its own `%fs` block. `errno`, `dlerror` and
`pthread_getspecific` then need purego's `internal/fakecgo`. Owning that as well
as the loader is a larger and less forgiving job than the loader itself, and the
interesting part of cppgo lies elsewhere.

That interesting part is the C++ layer. It has mangled symbols, implicit `this`, `const` methods, constructors, and knowing at generation time whether a binding
is possible at all.

## Why a slice is already shared memory

Because a `[]float64` already *is* shared memory.

When a slice crosses, cppgo passes a pointer to Go's own backing array and a
length. There is no copy, no serialisation, no second buffer: C++ reads and
writes the same bytes Go sees. The [stats example]({{< ref
"examples/stats.md" >}}) proves it with a pointer-identity check. For bulk data the bytes are already shared. The question is what protocol to put around them.

A full shared-memory design would add that protocol. A mapping to create, a lifetime to negotiate, a way to signal "the buffer is ready" and "I am done
with it", and a failure mode when one side dies. For a call that takes 13
cycles, that protocol costs orders of magnitude more than the call. For a
long-running or blocking operation it is the right shape, which is why the design says to move the C++ to a **separate process** for long or blocking work, where shared memory (or a pipe, or a socket) is how you talk to it.

The rule of thumb:

- **short, compute-bound call** — call it. The boundary is about 16 ns.
- **large data, short call** — pass the slice. No copy, no protocol.
- **long or blocking call** — another process, with a real transport.
- **per-element calls in a loop** — bind the loop instead. See [bulk
  data]({{< ref "tutorials/bulk-data.md" >}}).

## How a failure reaches Go

The C++ declaration decides. A function declared `noexcept` cannot throw, so it
is direct and its Go signature has no error. A function without `noexcept` goes
through a thunk, the thunk catches a `std::exception`, and the Go signature is
`(T, error)`.

The error is a `*cppgo.CallError` naming the declaration. Its `Unwrap` reaches
the `*cppgo.NativeError` with the message the shim stored. A call that reports
failure as a 32-bit status can be named with `-status`, and then the Go
signature returns only `error`.

`std::error_code`, `std::expected`, `errno` and the out-parameter conventions are
designed and tracked in [#19](https://git.manu343726.com/manu343726/cppgo/issues/19).
The [error handling page]({{< ref "manual/errors.md" >}}) has the full mapping,
with the measured rows and the designed ones marked.

## Why the generator reads the sources

The header declares the API. It leaves the presence of an out-of-line definition
unstated, and that definition is what gives a direct binding a symbol. A member
defined inside the class body is implicitly inline, and the library may export
nothing for it.

So the generator parses the header and, when it can find them, the translation
units that include it. A declaration seen only in a header can still be bound,
but the carrier changes. The inline case goes through the shim. The header alone is enough to produce a package. The sources decide which bindings take the fast path.

## Why no finalizer

A finalizer runs at an unpredictable time, on an arbitrary goroutine, possibly
while the library is unloading. Calling a destructor from there is a
use-after-free waiting to happen. The object's library may already be unloaded,
or another goroutine may be mid-call. The design chooses explicitness. Every
constructor has a `Close`, and Go calls it. `defer v.Close()` is the idiom.

## Can I bind a C library?

A C library's headers declare `extern "C"` functions, so mechanically yes. cppgo
parses them, and the generated package calls them.

The project is aimed at C++ though. The value of the direct carrier is the
mangled-name, `this` and validation work, and a C library needs none of it. For
a pure C library, cgo or purego directly is a better fit.

## Why one library per package

A generated package loads one shim, and the shim links one user library. That
keeps the symbol namespace small and `CppgoLibraryPath` meaningful. If you need
two libraries, use two binding packages.

Back to the [manual]({{< ref "manual/_index.md" >}}).
