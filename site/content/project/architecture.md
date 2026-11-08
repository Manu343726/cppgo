+++
title = "How it works"
description = "The path from a Go call to a C++ frame: the generated wrapper, the NOSPLIT trampoline, the stack switch, the purego loader, and why the P stays with the goroutine."
weight = 10
+++

## The path of one call

```
your Go code
  └── generated wrapper        idiomatic types: []float64, string, error
        └── generated asm      NOSPLIT, one per signature, no runtime involvement
              └── your C++     the symbol libclang reported, called directly
```

Nothing in that stack is written by hand at the boundary. The wrapper is
generated from your Go declaration. The trampoline is generated from the same
signature. The symbol is resolved from the C++ header. In the direct ABI mode
there is no `extern "C"` shim at all.

A call does five things:

```go
if err := cppgoEnsure(); err != nil { /* return or panic */ }
cppgo.ProbeStack()
cppgoRet := cppgo__ZNK5mathx6Vector4normEv(cppgoSymNorm, uintptr(v))
return cppgoRet
```

1. **Ensure** — on the first call, `dlopen` the library and `dlsym` every
   symbol. Afterwards, read the `sync.Once` result.
2. **Probe** — guarantee 32 KiB of goroutine stack headroom.
3. **Call** — the trampoline moves arguments into registers, switches the stack
   pointer, calls.
4. **Keep alive** — pin the Go memory the callee borrowed.
5. **Convert** — narrow or widen the result register to the declared Go type.

## The trampoline, and why it is assembly

```asm
TEXT ·cppgo__ZNK5mathx6Vector4normEv(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+16(FP)
	RET
```

Three properties matter, and each is a decision:

- **`NOSPLIT, $0`** — the trampoline allocates no memory and cannot be asynchronously preempted. `runtime.isAsyncSafePoint` returns false for any
  assembly function (`f.flag&abi.FuncFlagAsm != 0`), and for any `sp` below
  `gp.stack.lo`. The runtime will not try to unwind or grow a Go stack it never created. The tests validate this.
- **`ANDQ $~15, SP`** — SysV requires `RSP` to be 16-byte aligned *before* the
  `CALL`. Go's own ABI leaves that open. The trampoline establishes it.
- **The result is stored after `SP` is restored.** `FP` is relative to `SP`,
  so storing before the restore writes to the wrong address.

Arguments are read before the switch for the same reason. `R12` and `R13` are
callee-saved in SysV. The C++ function preserves the saved stack pointer and the symbol address across the call.

The trampoline never calls back into Go, never touches Go memory, and never
allocates. The runtime can leave it alone.

## Why not `asmcgocall`

The obvious shortcut would be to reuse the runtime's own stack switch:
`//go:linkname runtime.asmcgocall`. It is rejected by the Go linker
(`invalid reference to runtime.asmcgocall`). The switch is cppgo's own. That is also why the costs are cppgo's to document.

## The stack probe

A foreign function runs on the goroutine's stack. The Go runtime cannot grow
that stack while C++ is running, because the frame is invisible to it. So before
every call the wrapper guarantees headroom:

```go
func ProbeStack() { asm.Probe() }
```

The mechanism is an assembly function with a 32 KiB frame. The first call on a
goroutine grows its stack through the runtime's normal path. Every later call is
a frame adjustment. It is **not** a Go function with a `[32 << 10]byte` local,
because the compiler zeroes a large local array on entry (~1.2 µs per call,
measured), which would cost more than the call it protects.

`StackReserve` is 32 KiB. A C++ function that recurses or allocates a large
stack frame inside that window can still run past it. The reserve is a budget,
not a guard page.

## The loader

`internal/rtl` wraps `dlopen`/`dlsym` through
[`github.com/ebitengine/purego`](https://github.com/ebitengine/purego). No cgo,
no `import "C"`. `Library.Symbol` resolves a name. `LastError` calls
`cppgo_last_error` through the same trampoline mechanism.

purego is the only third-party dependency, and the decision to keep it is
recorded in the design contract. The short version. A Go program that calls into
libc has already left the world where thread-local storage is glibc-initialised,
and purego's `internal/fakecgo` is what makes `errno`, `dlerror` and
`pthread_getspecific` work anyway. Writing the loader would have meant owning
that too, a bigger and less forgiving job than the loader itself.

The libclang bindings in `internal/cxx` also use purego, but there they use its
**call path**, not just the loader. Libclang's C API passes `CXString`,
`CXType`, `CXCursor` and `CXSourceLocation` by value, and returns some of them
through a hidden pointer. purego classifies those aggregates correctly. That is exactly what the generated trampoline refuses to do for user bindings.

## What the collector sees

The GC never traces the C++ frame, because:

- an assembly frame cannot be preempted (`FuncFlagAsm`), and
- the foreign `SP` is outside `g.stack`. It cannot be mistaken for a Go stack.

The flip side is the design's central trade-off: **the `P` stays with the goroutine during the call.** cgo hands the `P` back to the scheduler and marks the goroutine as
being in a syscall. Cppgo keeps it. The goroutine is "running Go" from the scheduler's point of view while C++ executes. A foreign call in flight therefore
delays a GC cycle by up to its duration. Measured. A 2 ms call delayed the worst
GC cycle by **1.1 ms**, against **2.4 ms** for a spinning Go goroutine. (Same
frozen-CPU caveat as every number on this site. The machine was pinned to
964 MHz.)

That is the right trade for short, compute-bound calls. For a call that blocks, 
I/O, a lock, a long computation, move the work to another process, or at least
to a goroutine pool large enough that the delayed GC cycle becomes immaterial.

## Safety evidence

The properties above were validated, not just argued:

- `runtime.isAsyncSafePoint` returns false for any assembly function and for any
  `sp < gp.stack.lo`.
- GC torture: `GOGC=1` plus `runtime.GC()` in a loop under 32 goroutines.
- `SIGPROF`/`SIGURG` torture.
- `-race`, `GOMAXPROCS=1`, 32+ goroutines hammering calls.
- The `//go:linkname runtime.asmcgocall` rejection above.

The test suite runs twice in CI: once with `CGO_ENABLED=0` (proving the library
needs no cgo) and once with `-race` (which the toolchain refuses without cgo).
Neither pass replaces the other.

## The direct ABI mode, in one paragraph

The generator parses your Go package, asks libclang for each declared C++
entity's mangled symbol, parameter types, `const`-ness, virtual-ness and whether
it has an out-of-line body, and validates the Go signature against that. The
`this` pointer takes the first integer slot (after a reserved sret slot, if any),
floats go to XMM independently, strings and slices become `(pointer, length)`
pairs, and the result comes back in `RAX` or `XMM0`. If any of that cannot be
made to work, the build fails with the reason. That is the whole design. The
[manual]({{< ref "manual/_index.md" >}}) is the details.

Next is [benchmarks]({{< ref "project/benchmarks.md" >}}).
