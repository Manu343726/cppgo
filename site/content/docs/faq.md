---
title: "FAQ"
description: "Why not cgo, why not SWIG, why not purego's call path, why not always shared memory — plus build, deployment and compatibility questions."
weight: 100
---

## Why not cgo?

Because cgo is not a calling convention, it is a runtime integration. Every cgo
call does `cgocall`: hand the `P` to another thread, switch to `g0`, mark the
goroutine as being in a syscall, call, and undo all of it. That machinery exists
so that Go can be interrupted and collected while C code runs, and it costs
about **101 cycles** for a call that does almost nothing.

cppgo refuses the trade. It skips the handoff, so a call costs about **13
cycles** (7.5x cheaper), and it pays for that by not releasing the `P`: a
foreign call in flight delays a GC cycle by up to its duration. For short,
compute-bound calls that is a clear win. For calls that block, it is the wrong
tool and cgo is the right one.

There are two other consequences, and they are the ones people actually feel:

- **`CGO_ENABLED=0` works.** A cgo program requires a C toolchain at build
  time; a cppgo program requires one only for the C++ library you were going to
  compile anyway. Your Go binary stays static and single-file.
- **Cross-compilation keeps working**, because the Go side is pure Go plus
  assembly.

## Why not SWIG?

SWIG is a general wrapper generator, and it solves a much larger problem than
this one: it parses C and C++ headers, understands classes, templates,
overloads, inheritance and exceptions, and generates bindings for many target
languages.

That is also why it is not this: SWIG's output for Go goes through cgo — you
get `import "C"`, a C toolchain at build time, and the `cgocall` path on every
call. It also means the binding's shape is inferred from a header, rather than
declared in Go where you can read it. cppgo takes the opposite position: the
Go file *is* the binding, the wire format is an explicit contract
([ABI contract](/docs/abi-contract/)), and the generated C++ side is one
`extern "C"` declaration per function that you implement by hand. If you need a
whole class hierarchy bound automatically, SWIG or a cgo-based generator is the
better answer; if you need a small, fast, explicit boundary, this is.

## Why not use purego for the calls?

cppgo does depend on [purego](https://github.com/ebitengine/purego) — but only
for `Dlopen`, `Dlsym` and `Dlclose`. Its calling convention (`RegisterFunc`,
`SyscallN` and friends) is deliberately not used.

The reason is control, not speed. A cppgo call has to do two things a generic
"call this function pointer with these arguments" API cannot express:

- **Own the stack switch.** The trampoline reads its arguments before moving
  `SP`, calls with `RSP` aligned to 16 bytes, restores `SP`, and only then
  writes the result. That ordering is the contract, and it is why the
  trampoline is generated per signature instead of driven by a runtime argument
  block.
- **Make the stack guarantee explicit.** `ProbeStack` has to run immediately
  before the trampoline, on the calling goroutine, so the reserve is real at the
  point of the call.

The project publishes **no benchmark against purego's call path**, and you
should be suspicious of anyone who quotes one without measuring the same
function on the same machine. The argument here is about what the call has to
guarantee, not about which library is faster.

## Why not always use shared memory?

Because shared memory is a data-transfer mechanism, not a calling mechanism.

A separate process with a shared mapping still needs a way to say "run this
now, and tell me when you are done" — a futex, a pipe, a socket, a semaphore.
Every one of those is a syscall and a scheduler wakeup, and none of them can
call a C++ method on an object you already have loaded. You also lose the thing
that makes cppgo worthwhile in the first place: calling a function in the same
address space, with the pointer you already hold, for ~13 cycles.

What shared memory *is* good for is the case the design already points at: work
that is long or blocking. Then the syscall and the wakeup are noise next to the
work itself, the foreign code can take as long as it likes, and the `P`-release
problem disappears entirely because the call is not on the Go side any more.
Use it when the work is big. Do not use it to avoid a 13-cycle call.

## Does it really need no cgo?

Yes, and it is tested that way. `make test` runs two passes: one with
`CGO_ENABLED=0` to prove the no-cgo contract, and one with `CGO_ENABLED=1
-race` because `CGO_ENABLED=0 go test -race` is impossible — the toolchain
rejects it with `-race requires cgo`. Neither pass replaces the other.

## Can I cross-compile?

The Go side, yes: it is pure Go plus generated assembly, with no C toolchain
involved. The C++ library is a native artifact and has to be built for the
target — but that was already true before cppgo, and it is the only part of the
build that needs a C++ compiler.

## Do I have to ship the `.so` separately?

You can, and that is often the point: the library is `dlopen`'d at runtime, so
it can be swapped or versioned independently of the binary. You can also embed
it and extract it at startup if you want a single file — that is a packaging
decision, not a cppgo feature.

## Is the API stable?

The project calls itself early but complete end to end. The public surface is
small and documented in the [API reference](/docs/api/), and the generated
files are build artifacts — re-run `cppgo gen` after upgrading rather than
patching them. Pin the module version in `go.mod` if you need upgrades to be
deliberate rather than automatic.

## What happens if a symbol is missing?

`cppgo.Open` fails with an error naming the library, and `Symbol` fails with an
error naming the symbol — not a segfault at the first call. Libraries are opened
with `RTLD_NOW`, so a missing dependency is reported at open time as well.
`MustSymbol` is the variant that panics, and it exists only for generated
initialisation code, where a missing symbol is a fatal configuration mistake.

## Can C++ call Go?

Not in v1. There is no reverse trampoline. The recommended pattern is to
structure the C++ side as work that returns data, and let Go do the dispatch.
