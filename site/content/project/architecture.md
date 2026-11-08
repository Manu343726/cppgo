+++
title = "How it works"
description = "The two layers, the three artefacts, the three carriers, the callback ABI, the error mechanism, the two-phase template flow, the trampoline and the loader: how a generated Go call reaches a C++ frame."
weight = 10
+++

**Audience:** you have used cppgo and want the mechanism, or you are reviewing
the design. The reason the design is shaped this way is [Why
cppgo]({{< ref "project/why.md" >}}). This page is the how. The line-level detail
of each generated block is in [the generated files]({{< ref
"reference/generated-code.md" >}}).

## The two layers

| layer | package | owns | may depend on |
|---|---|---|---|
| **runtime** | `github.com/Manu343726/cppgo` (repository root) | loading, symbol resolution, the C ABI and its trampolines, the reentrant entry, stack probing, `Object`, the wire helpers, the error types, the callback table | the standard library, `internal/asm`, `internal/rtl`, `internal/callback`, `purego` |
| **generator** | `internal/gen`, `internal/cxx`, `cmd/cppgo` | parsing C++ through libclang, flag discovery, selection, carrier routing, emitting Go and assembly, emitting the shim, and the shim's own C++ compile | anything, including libclang through purego and the C++ compiler |

The runtime never imports `internal/cxx` and has no libclang dependency. libclang
is generation-time only. The runtime builds and tests with `CGO_ENABLED=0` on a
machine with no libclang, and the generator reports `ErrLibclangNotFound` when
the parser is absent and a compiler error when the shim cannot be built.

## The three artefacts

| artefact | who owns it | what it is |
|---|---|---|
| **the user's library** | you | your sources, your flags, your build. cppgo never writes into it and never asks it to include anything |
| **the cppgo shim** | cppgo | `zz_cppgo_shim.cpp`, generated from the same header parse as the Go API, compiled by cppgo into `libcppgo_<package>.so`, and linked against your library. One `extern "C"` thunk per binding the direct carrier cannot express |
| **the generated Go package** | cppgo | the API you import, one `NOSPLIT` trampoline per binding, and `Open`, which loads the shim and with it your library |

The dotted line is the contract's edge. Your build stays untouched, and no
generated code goes into it. A consumer installs two shared objects, the shim and
your library, and a header-only library has only the shim.

The artefacts carry no build path. The shim names your library by **soname** in
its `DT_NEEDED` entry and is linked with `-Wl,-rpath,$ORIGIN`. The two objects
resolve each other when they sit in one directory, and the generated `Open`
looks for your library at `CPPGO_LIBRARY_PATH`, then `CppgoLibraryPath`, then
beside the shim, then on the loader's search path.

## One call, three carriers

Every binding gets exactly one carrier before anything is emitted. The carrier is
invisible in the API and named in the report.

```
your Go code
  └── generated wrapper              []float64, string, error, *Vector
        ├── direct trampoline       NOSPLIT asm → mangled symbol in your library
        ├── thunk trampoline        NOSPLIT asm → extern "C" thunk in the shim → your library
        └── reentrant call          runtime.cgocall → generated asm → thunk → your library
```

### Direct

The generated trampoline calls the mangled C++ symbol in your library. All of
these must hold:

- the declaration is **`noexcept`**. The direct path has no handler, and an
  escaping exception terminates the process.
- it is **not virtual**. The call goes to a fixed address, and dispatch is the
  shim's job.
- the signature is **register-representable**: scalars, enums, pointers and
  references, and a POD of at most 16 bytes with no MEMORY-class part. No
  `std::` type.
- the symbol is **exported**. The header shows an out-of-line body, or the
  library's `.dynsym` has the name.

This is the fast path: **15.9 ns/op**, measured 2026-11-08 with
`go test -bench . -benchtime 200000x` (DESIGN.md, The trampoline).

### Thunk

Everything else is carried by a C++ function generated into the shim and compiled
by cppgo. The mechanisms below are measured in `internal/shim`. The constructs
the emitter binds today are listed on the [limitations page]({{< ref
"project/limitations.md" >}}).

| declaration | what the thunk does |
|---|---|
| inline or header-only body | includes the header and calls it. The body is compiled into the shim |
| virtual method | calls it as C++ does, dispatching through the vtable |
| template | instantiates the concrete specialisation and calls it |
| `std::string` or `std::vector` | constructs the C++ value, copies in or out |
| record by value | copies it into the call, and constructs a result in Go storage through the `sret` pointer |
| exception-throwing call | wraps the call in `try`/`catch` and stores the message |
| multiple inheritance | performs the `static_cast`/`dynamic_cast` that adjusts `this` |

A thunk adds one C++ call frame. The Go-side trampoline is the same as the direct
one. A non-`noexcept` thunk returns `(T, error)`, and a `noexcept` one has no
error result.

### Reentrant

A binding whose signature takes a function pointer or a `std::function` cannot
use the direct trampoline. The runtime only accepts a callback on a thread
already inside a foreign call. The call goes through `runtime.cgocall`, reached
by linkname so there is still no cgo at build time, with a generated assembly
wrapper that loads the six INTEGER and eight SSE registers.

- **A callback reached through the direct trampoline kills the process**, with
  `fatal error: morestack on g0`. That is measured in a child process, and it is
  why the carrier is chosen from the signature.
- **It costs about ten times a direct call**: 151 ns/op against 15.9 ns/op on the
  same machine, plus a 16-byte allocation per call. The argument block cannot
  live on a goroutine stack a callback may grow.
- **The C frames run on the thread's stack**, so the 32 KiB stack probe is not
  what protects them.

## The callback ABI

A Go function handed to C++ is a slot in a process-wide table of 64. The slot
holds a five-byte `CALL` instruction, and the instruction names the slot. The
runtime's callback entry runs the Go function on the goroutine that made the
call, locked to its M, with the C frames left where they are.

- **Arguments:** `RDI`, `RSI`, `RDX`, `RCX`, `R8`, `R9` and `XMM0`..`XMM7`, the
  register file only. A callback taking a stack-passed argument is a generation
  error naming the sixth argument and the fix.
- **Result:** `RAX` or `XMM0`, one register.
- **Lifetime:** registering twice gives two slots. `ReleaseCallback` waits for
  the calls already inside the Go function to return. A C++ object that calls a
  released address reaches the dispatcher's released-slot path and returns a
  zero result. That is a programmer error on the C++ side and never a
  use-after-free.
- **Panic:** the dispatcher recovers it, zeroes the result and records it. The
  binding re-panics in Go after the foreign call returns. Measured.
- **Reentrancy:** Go to C++ to Go to C++ nests. The runtime counts foreign calls
  per M.

## Errors

The mechanism lives in the shim, because the shim is C++ and can catch. Today:

- a thunk wraps the call and its marshalling in `try`/`catch`.
- a caught `std::exception`, or any other throw, stores its message in a
  thread-local string and returns a non-zero status.
- the generated wrapper reads `cppgo_last_error()` through the same trampoline
  mechanism and returns `*cppgo.CallError` wrapping `*cppgo.NativeError`.
- `-status` reads a 32-bit integer return as success or failure instead.

`std::error_code`, `std::expected`, `std::optional`, `errno` and the `bool` plus
out-parameter convention are specified in [`DESIGN.md`](DESIGN.md) and tracked as
[#19](https://git.manu343726.com/manu343726/cppgo/issues/19). The projection and
its configuration are measured in `internal/errormap`. The generator has not
emitted them yet. [Error handling]({{< ref "manual/errors.md" >}}) has the table,
with the measured rows and the designed ones marked.

## Templates and the two-phase flow

A C++ template exists only where it is instantiated. The design makes that a
two-phase flow:

1. the Go program instantiates the generic API (`Matrix[float64,4,4]`).
2. cppgo collects the instantiations, from the Go package's type arguments or
   from an explicit list.
3. cppgo emits the matching C++ specialisations into the shim, compiles it, and
   the Go package calls the thunks.
4. a Go instantiation with no C++ counterpart is a load-time error naming the
   exact `template …;` line to add.

What the emitter does today is the concrete API, through `-instantiate
'tmpl::Cell<int>'`, which produces `CellInt` and its methods. The generic Go API
and the automatic collection are the next phase, 4b-1, under epic #1.

## The generated identifiers

A generated binding holds two names. They are the resolved symbol, in
`cppgoSym_<hash>`, and the trampoline, in `cppgoCall_<hash>`. The hash ties them
to the qualified signature. This is the real shape of a thunk-carried method from the
project fixture:

```go
func (s *Signal) Gain() (float64, error) {
	if s == nil || s.obj == nil {
		return 0, cppgo.ErrClosed
	}
	if err := cppgoOpen(""); err != nil {
		return 0, err
	}
	cppgo.ProbeStack()
	var cppgoResult float64
	cppgoStatus := cppgoCall_f522a9b5b52d(cppgoSym_f522a9b5b52d, s.cppgoAddr(), unsafe.Pointer(&cppgoResult))
	cppgo.KeepAlive(s, &cppgoResult)
	if cppgoStatus != 0 {
		return 0, cppgoCallError("analytic::Signal::gain")
	}
	return cppgoResult, nil
}
```

`cppgoSym_<hash>` is the resolved address, `cppgoCall_<hash>` is the NOSPLIT
trampoline, and the hash comes from the qualified signature. A direct binding
has no status and no out-pointer. A constructor allocates the measured size and
releases it if the constructor throws. A callback binding fills a `cppgo.Args`
frame and calls `cppgo.CallReentrant`. The full set of wrapper shapes is in
[the generated files]({{< ref "reference/generated-code.md" >}}).

## The trampoline, and why it is assembly

```asm
// func cppgoCall_eafad677153b(fn uintptr, this uintptr, a1 float64) float64
TEXT ·cppgoCall_eafad677153b(SB), NOSPLIT, $0-32
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVSD	a1+16(FP), X0
	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+24(FP)
	RET
```

Three properties matter, and each is a decision:

- **`NOSPLIT, $0`**. The trampoline allocates no memory and cannot be
  asynchronously preempted. The runtime will not try to unwind or grow a Go stack
  it never created.
- **`ANDQ $~15, SP`**. SysV requires `RSP` to be 16-byte aligned before the
  `CALL`. Go's own ABI leaves that open, so the trampoline establishes it.
- **The result is stored after `SP` is restored**. `FP` is relative to `SP`, so
  storing before the restore writes to the wrong address.

Arguments are read before the switch for the same reason. `R12` and `R13` are
callee-saved in SysV. The C function preserves the saved stack pointer and the
symbol address across the call.

The obvious shortcut, reusing the runtime's `asmcgocall` by `go:linkname`, is
rejected by the Go linker. The switch is cppgo's own, and that is why the costs
are cppgo's to document.

## The stack probe

A foreign function runs on the goroutine stack for the direct and thunk carriers,
and the runtime cannot grow a stack it cannot describe. Before every call the
generated wrapper runs `ProbeStack`, an assembly function with a 32 KiB frame:

- the first call on a goroutine grows its stack through the runtime's normal
  path, and every later call is a frame adjustment.
- it is not a Go function with a `[32 << 10]byte` local, because the compiler
  zeroes a large local on entry (about 1.2 µs per call, measured), which would
  cost more than the call it protects.
- it must run from the same goroutine, before any pointer into the caller's
  frame is taken. The probe can move that frame, and taking the address first is
  the bug that broke the first draft of the shim tests.

`StackReserve` is 32 KiB. A C++ function that recurses or allocates a large stack
frame inside that window can still run past it. The reserve is a budget, and the
reentrant carrier runs on the thread's stack with the thread's budget instead.

## The loader

`internal/rtl` wraps `dlopen` and `dlsym` through
[`github.com/ebitengine/purego`](https://github.com/ebitengine/purego). No cgo,
no `import "C"`. The generated `Open` loads the shim. The shim names your
library in its `DT_NEEDED` entry, and the loader resolves it through the shim's
`DT_RUNPATH`. The run path is `$ORIGIN`.

purego is the only third-party runtime dependency. The reason is in [Why
cppgo]({{< ref "project/why.md" >}}). A non-cgo program has no glibc thread
control block, and purego's `internal/fakecgo` makes `errno`, `dlerror` and
thread-specific data work anyway.

`cppgo.OpenFS` adds embedding. Every `.so` in an `fs.FS` is extracted to a
private per-user cache and the named one is loaded. It is the deployment path
for `//go:embed`. Wiring it into a generated package's `Open` is
[#20](https://git.manu343726.com/manu343726/cppgo/issues/20).

## What the collector sees

The GC never traces the C++ frame, because an assembly frame cannot be preempted
and the foreign `SP` is outside `g.stack`. The flip side is the central
trade-off. **The `P` stays with the goroutine during a direct or thunk call.**
cgo hands the `P` back and marks the goroutine as being in a syscall. cppgo keeps
it. A foreign call in flight delays a GC cycle by up to its duration. The measurement is a worst GC delay of **1.1 ms** for a 2 ms call, against
**2.4 ms** for a spinning Go goroutine ([benchmarks]({{< ref "project/benchmarks.md" >}})).

This is right for short, compute-bound calls. For a call that blocks (I/O, a
lock, a long computation), move the work to another process.

## Safety evidence

The properties above are measured:

- `runtime.isAsyncSafePoint` returns false for an assembly function and for any
  `sp < gp.stack.lo`.
- GC torture with `GOGC=1` and `runtime.GC()` in a loop under 32 goroutines.
- `SIGPROF`/`SIGURG` torture, `-race`, `GOMAXPROCS=1`, and 32 or more
  goroutines.
- every fixture package is built with `go vet`, whose asmdecl checks each
  trampoline's frame size and offsets against its Go declaration.
- the test suite compiles the shim. That is how "cppgo compiles the shim" is
  measured against a real compiler.

The suite runs twice. `CGO_ENABLED=0` proves the no-cgo contract, and
`CGO_ENABLED=1 -race` runs the race detector, which the toolchain refuses with
cgo disabled. Neither pass replaces the other.

## Determinism

The generated Go and C++ are byte-identical for the same inputs. Bindings are
sorted by qualified name, and thunk names come from a stable hash of the
qualified signature. The compiled shim is a build artefact, verified by compiling
it and never by comparing binaries. `cppgo check` compares sources.

Next is [benchmarks]({{< ref "project/benchmarks.md" >}}).
