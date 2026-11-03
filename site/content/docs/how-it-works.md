---
title: "How it works"
description: "The generated trampoline, why it is NOSPLIT, why it never touches the runtime, and why the cgocall path had to be replaced."
weight: 30
---

The call stack of a cppgo binding is four layers deep, and only the middle two
are generated:

```
user code
  └── generated Go wrapper  (idiomatic types: []float64, string, error)
        └── generated asm trampoline   (NOSPLIT, per signature)
              └── generated C++ shim   (extern "C", ABI-stable, catches exceptions)
                    └── the user's C++ code
```

The wrapper does the boring work: turn a slice into a pointer and a length,
check a status, copy an error message into a Go string, keep the borrowed
memory alive. The C++ shim does the defensive work: catch `std::exception` and
`...` so nothing unwinds into a Go frame, and store the message where Go can
find it. The trampoline in the middle is the part with no alternative
implementation, and it is deliberately tiny.

## The trampoline

```asm
TEXT ·Dot(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	a+8(FP), DI
	MOVQ	n+16(FP), SI
	MOVQ	SP, R13
	ANDQ	$~15, SP        // SysV: RSP 16-aligned *before* the CALL
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+24(FP)
	RET
```

The runtime uses a general version of the same thirteen instructions — one
trampoline for all signatures, taking the argument block as a pointer — while
generated code emits a specialised one per signature so no argument marshalling
happens at run time. Both follow the same rules.

### Rule 1: read the arguments before the stack switch, store the result after

`FP` (the pseudo-register Go's assembler uses for arguments and results) is
relative to `SP`. The moment `SP` is re-pointed at the foreign stack, every
`FP` offset means something else. So the trampoline loads every argument into a
register first, and stores the result only after `SP` has been restored. Get
this wrong and you do not crash immediately — you write to the wrong address and
find out much later.

### Rule 2: the frame is `$0` and `NOSPLIT`, and that is load-bearing

The trampoline must not allocate, must not touch Go memory, and must not call
back into Go. `$0` means it has no frame of its own; `NOSPLIT` means the
prologue does not check the stack guard and does not try to grow the stack.
Both are required, because the stack pointer is foreign for most of the
function's life and the runtime's stack-growth path would be operating on a
stack it does not own.

### Why the stack is switched at all

Because the C++ ABI expects a stack it can use. The goroutine's stack is a Go
stack: it can move, it is bounded, and the runtime needs to know where its
bottom is. Handing that pointer to foreign code and not switching would leave
`g.stack.lo` pointing somewhere the C++ frames are free to grow past.

The `ANDQ $~15, SP` is the System V requirement: `RSP + 8` must be 16-byte
aligned at the callee's entry point, which means `RSP` must be 16-byte aligned
immediately before `CALL`. Aligning down inside the goroutine's existing
headroom is enough, because `cppgo.ProbeStack()` has already guaranteed at
least `cppgo.StackReserve` (32 KiB) of room below the caller.

`R12` and `R13` carry the target and the saved stack pointer across the call
precisely because System V makes them callee-saved. `R14`, which holds `g` in
the Go ABI, is never touched.

## Why not reuse `runtime.asmcgocall`?

That was the first attempt, and it does not work. The Go linker rejects the
reference:

```
//go:linkname runtime.asmcgocall
invalid reference to runtime.asmcgocall
```

`asmcgocall` is runtime-internal by construction, and `linkname` is not a
supported way around that. There is no supported entry point into the runtime's
C-call machinery from outside the runtime, so the switch is ours: thirteen
instructions we own and can read.

## Why the runtime leaves the goroutine alone

Two independent properties do the work, and both are worth knowing because
they are the reason this is not a game of chance:

1. **`runtime.isAsyncSafePoint` returns false for any assembly function**
   (`f.flag & abi.FuncFlagAsm != 0`). A `NOSPLIT` assembly trampoline is never
   asynchronously preempted, so the runtime will not stop the goroutine while
   its stack pointer is foreign.
2. **`isAsyncSafePoint` also returns false for any `sp < gp.stack.lo`.** Once
   the trampoline has moved `SP` below the goroutine's stack, a foreign stack
   pointer is never mistaken for a Go stack, even if something else asks.

So the GC does not scan the goroutine's stack while foreign frames are on it,
and the preemption signal does not land in the middle of the call. The
goroutine simply waits at the trampoline's return, and resumes normally when
the C++ side returns.

## What the runtime does *not* do

The trampoline does not call `entersyscall`, does not hand off the `P`, does
not switch to `g0`, and does not mark the goroutine as being in a syscall. That
is the entire performance argument: those steps are what cgo pays for on every
call, and skipping them is where the ~101 cycles become ~13.

It is also the entire cost. Because the `P` is not released, a foreign call in
flight delays a GC cycle by up to its duration. For a short, compute-bound call
that is a rounding error. For a call that blocks, it is the wrong tool — see
[limitations](/docs/limitations/).

## Symbol resolution

`dlopen`/`dlsym` are provided by
[`github.com/ebitengine/purego`](https://github.com/ebitengine/purego), which is
cppgo's only third-party dependency. Only `Dlopen`/`Dlsym` are used: the
library's own calling convention (`RegisterFunc`, `SyscallN` and friends) is
deliberately not, because cppgo enters the C ABI from its own assembly. Libraries
are opened with `RTLD_NOW | RTLD_LOCAL`, so a missing dependency fails at open
time rather than at the first call.

The wrapper's `ProbeStack()` call is the last piece: it runs *before* the
trampoline, and it forces the goroutine's stack to grow to at least the reserve
so that the foreign frames cannot grow past `stack.lo`. See
[safety](/docs/safety/) for what it guarantees and how it is measured.
