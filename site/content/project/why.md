+++
title = "Why cppgo"
description = "The problem cppgo solves: a Go program that needs a C++ library, what the cgo route costs and why, what cppgo does instead, and when you should not use it."
weight = 5
+++

**Audience:** a Go developer who has never built a foreign-function interface, 
an FFI, the layer that lets code written in one language call code compiled from
another, and wants to know whether this project is for them. Each term is
defined the first time it appears. The page that owns a mechanism is linked
instead of repeated, and the [glossary]({{< ref "manual/glossary.md" >}})
collects the one-sentence definitions.

## The short answer

You have a C++ library. You have a Go program that needs it. The ordinary routes
are to write C wrappers and call them from Go with cgo, or to reimplement the
library in Go. cppgo is a third route. Point it at the C++ headers and the
compilation flags your build already uses, and it generates the Go package that
calls the compiled library directly, no  `import "C"`, no `CGO_ENABLED=1`, and
no C wrapper you have to write and keep in sync.

It is the right tool when the boundary is part of your workload. Many calls,
each of them short. It is the wrong tool when you call into C a handful of times
per program, or when the calls block. The [last
section](#when-to-use-cppgo-and-when-not) is the decision list. The rest of this
page is why the trade falls where it does.

## Two runtimes in one process

A Go program does not hand its work straight to the operating system. It has its
own runtime, and four of its pieces matter here:

- **Goroutines** are the unit of work you write. They are much cheaper than
  operating-system threads, and the runtime multiplexes many of them onto a few
  threads.
- **The scheduler** decides which goroutine runs where. Its bookkeeping has three
  letters: `G` is a goroutine, `M` is an operating-system thread, and `P` is the
  *processor*, the token a thread needs in order to run Go code. Only
  `GOMAXPROCS` goroutines run Go code at once because there are only that many
  `P`s to hold. A goroutine with no `P` is parked.
- **Goroutine stacks are small and grow.** A Go stack starts at a few kilobytes
  and the runtime copies it to a bigger buffer when a function needs more room.
  It can only do that for functions it compiled and can inspect.
- **The garbage collector** (GC) walks the stacks of all goroutines to find
  pointers. It can only do that while the goroutines are stopped at a point it understands. The runtime's preemption machinery provides that point.

Now call a C++ function. Everything above stops applying for the duration of
that call:

- The C++ frame is laid out by a C++ compiler, and the Go runtime has no
  description of it. The runtime cannot walk it for pointers and **cannot grow it**.
  The stack has to be big enough before the call begins.
- The C++ function exists outside the goroutine model. Whether the scheduler can use the calling
  thread for other work while C++ runs is a decision the caller makes, and that
  decision is the central trade-off of any non-cgo FFI (this one included).
- When the C++ call returns, the result arrives in the registers the platform's
  calling convention defines, the fixed agreement about which CPU register
  carries which argument. Nothing in that agreement knows about Go types.

cppgo's job is to make those three facts cost as little as possible. How the
generated code does it, the trampoline, the stack probe, what the collector
sees, is [How it works]({{< ref "project/architecture.md" >}}), and this page
links to it rather than repeating it.

## What cgo does, and what it costs

cgo is Go's supported way to call C. It is mature and it works. This project
makes no claim that it is broken. It is worth understanding what it does, because the
cost is in the mechanism.

You write a comment block above `import "C"`:

```go
/*
#include <math.h>
*/
import "C"

func Sqrt(x float64) float64 { return float64(C.sqrt(C.double(x))) }
```

The Go toolchain reads that comment, invokes a C compiler, and generates the glue
that crosses the boundary. At run time each call goes through the runtime's
`cgocall` path, which in Go 1.27 does three things before your C function runs
([`src/runtime/cgocall.go`](https://github.com/golang/go/blob/master/src/runtime/cgocall.go)):

1. `entersyscall` — the goroutine is marked as being in a system call and its
   `P` is handed back. The scheduler can run other goroutines while this call
   is in flight.
2. `asmcgocall` — the stack is switched to the thread's `g0` stack, the special
   stack the runtime uses for its own bookkeeping.
3. the C function is called. On return, `exitsyscall` reacquires a `P` (possibly
   on a different thread) before any Go code runs again.

That handshake is what keeps the scheduler and the collector correct with an
arbitrary foreign frame on the stack, and it is what you pay per call. Measured
on the same `dlopen`'d function, in the same process: **105 ns / about 101
cycles** through cgo, against **13.9 ns / 13 cycles** through a cppgo trampoline
and **2.1 ns / 2 cycles** for a plain indirect Go call. The commands, the pinned
clock and what the numbers do *not* cover are on the
[benchmarks page]({{< ref "project/benchmarks.md" >}}). Read the cycle counts
there.

There is a second cost, and for C++ it is the larger one. cgo speaks **C**, and a
C++ library exports no C entry point of its own:

- **Names.** A C++ compiler exports a mangled name for `mathx::Vector::norm`.
  It *mangles* it, encodes namespaces, the class, the parameter types and the
  `const` qualifier into one symbol such as `_ZNK5mathx6Vector4normEv`. That is how
  overloads coexist. A C symbol is a plain identifier. Every C++ function
  you want to call from C has to be wrapped in a function declared
  `extern "C"`, which turns the mangling off.
- **Types.** In that wrapper, `this`, the hidden pointer to the object a member
  function works on, becomes a  `void *`, a class becomes an opaque handle, and
  `std::string` becomes whatever you decide to copy it into. The wrapper is a
  hand-written projection of a rich API onto C, and it is the part that goes
  stale. The header changes, the wrapper stays behind, and the compiler cannot tell
  you.

Writing that layer by hand for a large library is the actual work of binding C++
with cgo. It is also the work nobody wants to repeat.

## What cppgo does instead

cppgo removes the hand-written C layer. Where one is still needed, it generates
and compiles it. For a bound header it:

1. parses the C++ **header** (the file that declares the API, without the bodies)
   with libclang, the same C++ parser clangd and the clang compiler use, loaded
   as a shared library, and asks it for the real declarations. Parameter types,
   sizes, `const`-ness, virtual-ness, exception specification, and whether a
   definition exists.
2. resolves each declaration to the **mangled symbol** the C++ library exports,
   or decides that the direct call cannot express it.
3. emits a Go wrapper that converts idiomatic Go values (`[]float64`, `string`,
   `error`) into what the call needs, and **one small assembly function per
   bound signature** that puts the arguments in the registers the calling
   convention defines and calls the symbol directly.
4. for everything the direct call cannot express, emits one `extern "C"`
   **thunk** into a C++ translation unit, `zz_cppgo_shim.cpp`, **compiles that
   unit itself** into `libcppgo_<package>.so`, and links it against your
   library. Your build never sees it.

That assembly function is the *trampoline*: a few instructions that bounce the
call into the C++ code and back without the runtime's help. It is marked `NOSPLIT`, meaning it allocates no memory and asks the runtime for no stack growth. That makes it safe to run without the scheduler's `cgocall` handshake. The thunk is the safe, general path. Being C++, it can call an inline
body, dispatch a virtual method through the **vtable** (the per-object table of
function pointers a virtual call reads), construct a `std::string`, and catch an
exception.

The three carriers, direct trampoline, thunk, and a third *reentrant* call for
signatures that pass a Go function to C++, are the subject of [How it
works]({{< ref "project/architecture.md" >}}).

The consequences you care about:

- **`CGO_ENABLED=0` builds and tests**, and no C compiler is needed to build the
  Go side. Cross-compiling needs no C cross-compiler.
- **The C++ API keeps its shape.** A member function is a Go method, a
  constructor is `NewXxx`, a destructor is `Close`, and a slice is passed as a
  pointer and a length into the C++ code that already expected them.
- **The library is loaded at run time** with `dlopen`. The same binary can
  load a different build of it.
- **You write no bindings.** The input is the header and the flags. The output
  is a package you import.

This is not free. *What it costs you* is the bill. The section before it explains
a dependency that surprises people.

## The dependency that looks contradictory

`dlopen` and `dlsym`, the two C library functions that load a shared object and
look up a symbol in it, are themselves C functions. cppgo calls them without
cgo, through [`github.com/ebitengine/purego`](https://github.com/ebitengine/purego),
the project's only third-party runtime dependency.

An FFI that depends on another FFI is a fair joke. Here is the reason, also
recorded in the design contract's *Loading* section
([`DESIGN.md`](https://github.com/Manu343726/cppgo/blob/main/DESIGN.md)):

A Go binary built without cgo has no C runtime initialised around it. Glibc
keeps per-thread data,  `errno`, the thread's last loader error, thread-specific
values, in a **thread control block** it sets up when a thread starts. A Go
program installs its own thread-local storage for the goroutine bookkeeping, so
C library code that reaches for that block finds a structure glibc never
initialised. purego solves this in its `internal/fakecgo` package. That package makes `errno` and the loader's error reporting work from a non-cgo Go program.

Writing the loader by hand would mean owning that problem too. Per-platform
assembly entry points, `//go:cgo_import_dynamic` directives, and a loader that
breaks quietly when a libc changes its symbol versions. That is a larger and less
forgiving job than the loader itself, and the interesting part of this project
lies elsewhere. The interesting part is the C++ layer. Mangled symbols, implicit `this`,
`const` methods, constructors, and knowing at generation time whether a binding
is possible at all.

## What it costs you

Being explicit about the trade-offs is the point of the project, so:

- **The `P` stays with the goroutine during a direct call.** cppgo skips the
  `entersyscall` handshake. From the scheduler's point of view the goroutine
  is still running Go while C++ executes. A foreign call in flight therefore
  delays a GC cycle by up to its duration. Measured. A **2.0 ms** call delayed
  the worst GC cycle by **1.1 ms**, against **2.4 ms** for a Go goroutine that
  spins instead of yielding. Right for short, compute-bound calls. Wrong for
  calls that block. The measurement and its clock caveat are on the
  [benchmarks page]({{< ref "project/benchmarks.md" >}}).
- **The foreign frame lives on the goroutine stack** for a direct call, inside a
  32 KiB reserve that the generated wrapper establishes before every call. Deep
  recursion or a large local array in the C++ function can exhaust it. The
  reserve is a budget. A thunk call adds one C++ frame inside
  that budget. The reentrant carrier runs on the thread's stack instead.
- **A callback-capable binding is about ten times the cost of a direct one** —
  151 ns against 15.9 ns, measured 2026-11-08, because the argument block
  cannot live on a goroutine stack that a callback may grow. A callback carries
  at most six integer and eight floating-point arguments and one register
  result.
- **A binary that loads a shared library at run time is dynamically linked by
  definition.** The promise covers the build, with no cgo and no C toolchain.
- **The C++ ABI and the standard library are part of the contract.** The
  trampoline is written for the System V AMD64 calling convention (the agreement
  used by Linux on x86-64) and it calls symbols compiled by *your* C++ compiler,
  so the library's own ABI, compiler family, standard library, platform, is
  fixed when you build it. Today that means Linux/amd64. The generator rejects a
  shim and a library that mix libstdc++ and libc++.
- **An exception can only be caught in a C++ frame.** The generated shim has
    one. A throwing function works there. A direct binding is `noexcept` by
  construction. A throw with no C++ frame above it terminates the process, and
    that is measured.
- **Every object needs an explicit `Close`.** There is no finalizer. Go decides
  when a C++ object dies.

## Where this is going

The generator is header-first today. The parts that are designed but not
emitted yet are marked on the pages that describe them:

- **the generic Go API for templates** — a C++ template exists only where it is
    instantiated. The flow is two-phase. The Go program instantiates
  `Matrix[float64,4,4]`, cppgo collects the instantiations and emits the matching
  C++ specialisations. Concrete specialisations work today through
  `-instantiate`. The generic Go API is the next phase (tracked under the v4
  epic, #1).
- **the full error projection** — `std::error_code`, `std::expected`,
  `std::optional`, `errno`, status ints and `bool` + out-parameters become Go
  errors through the shim. The mechanism is measured in `internal/errormap`.
    the generator has not emitted it yet ([#19](https://git.manu343726.com/manu343726/cppgo/issues/19)).
- **the target libraries** — Eigen, OpenCV and ArrayFire as generated bindings
  tested against the real libraries, with build tags. They are the acceptance
    tests for the whole design and wait for their implementation.
- **embedded deployment** — `cppgo.OpenFS` extracts `.so` files embedded with
  `//go:embed` into a private cache and loads them. The runtime ships it. Wiring
  it into a generated package's `Open` is [#20](https://git.manu343726.com/manu343726/cppgo/issues/20).

## When to use cppgo, and when not

Use it when:

- **you have an existing C++ library and want an idiomatic Go API to it**, without
  cgo in your build and without maintaining a hand-written `extern "C"` layer.
- **the boundary is on the hot path**, with per-element access and many short
  calls. cgo's ~100 cycles per call would show up in the profile.
- **you can ship a dynamically linked binary** and accept the C++ ABI and standard
  library coupling that comes with it.
- **you need the full C++ surface.** Classes with methods,
  `const` members, constructors, virtual dispatch, `std::` types, and the type
  checking that comes from parsing the real header.

Do not use it when:

- **you call into C a few times per program.** cgo is mature, supported by the
  toolchain, and simpler to explain to the next maintainer. Use it.
- **the calls block** — I/O, a lock held across the call, a long computation.
  Every millisecond in there is a millisecond the collector cannot finish a cycle.
  Move that work to a separate process and talk to it over a pipe, a socket, or
  shared memory.
- **your library is plain C.** The direct carrier's value is the mangled-name,
  `this` and validation work, none of which a C library needs. cgo, or purego
  directly, is the better fit.
- **you need arm64 or Windows.** The runtime is Linux/amd64. The package builds
  elsewhere and panics with a clear message.

If the answer is "use it", the [installation page]({{< ref
"manual/installation.md" >}}) is the first step and the
[quickstart]({{< ref "manual/quickstart.md" >}}) is one class from header to
running program. If you would rather understand the boundary before writing code,
read [How it works]({{< ref "project/architecture.md" >}}).
