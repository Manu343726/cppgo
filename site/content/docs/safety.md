---
title: "Safety"
description: "How cppgo keeps the garbage collector, the preemption signal and the goroutine stack out of the foreign call — what is guaranteed, and what is not."
weight: 60
---

A foreign call from Go is a place where three runtime invariants can break at
once: the collector can decide to scan a stack it cannot read, the preemption
signal can land where there is no Go frame to stop, and the stack can be asked
to grow by code that does not know it is on one. cppgo addresses all three
deliberately, and this page states which parts are guarantees and which are
the consequences you have to keep in mind.

## The three problems

**The collector.** The GC needs to know, for every goroutine, where its stack
is and which words on it are pointers. During a cppgo call the goroutine's
stack contains C++ frames — no pointer maps, foreign calling conventions, and a
stack pointer that is not the goroutine's.

**Preemption.** Go's asynchronous preemption uses `SIGURG` (and profiling uses
`SIGPROF`). A signal that arrives while the goroutine is inside C++ code has to
be handled without the runtime assuming the interrupted frame is a Go function.

**The stack.** A goroutine's stack starts small and grows on demand by copying.
Growing it while C++ frames are on it would move memory out from under the
foreign code.

## What makes it safe

**The trampoline is never asynchronously preempted.** `runtime.isAsyncSafePoint`
returns `false` for any assembly function — the check is on
`f.flag & abi.FuncFlagAsm` — so a `NOSPLIT` assembly trampoline is not a place
the runtime will try to stop the goroutine. There is no Go frame at the point of
interruption, and the runtime knows it.

**A foreign stack pointer is never mistaken for a Go stack.**
`isAsyncSafePoint` also returns `false` for any `sp < gp.stack.lo`. Once the
trampoline has moved `SP` below the goroutine's stack, nothing can conclude that
the current stack is the goroutine's own.

Together those two properties mean the collector skips the goroutine while the
call is in flight, and the preemption signal does not try to unwind through it.
The goroutine waits at the trampoline's `RET` and resumes when the C++ side
returns — exactly like any other call, just with a longer wait.

**The stack has room before the call starts.** `cppgo.ProbeStack()` guarantees
at least `cppgo.StackReserve` bytes of headroom below the caller, so the C++
frames cannot grow past `stack.lo` and the runtime never has to grow a stack
that contains foreign frames.

## How `ProbeStack` works

Go grows a goroutine's stack when a function is entered whose frame does not
fit. So the guarantee is obtained the only way Go permits: by calling a function
with a frame that large and touching it.

```go
//go:noinline
func ProbeStack() {
	var probe [StackReserve]byte

	probe[0] = 1
	for i := 4096; i < StackReserve; i += 4096 {
		probe[i] = probe[i-4096] + 1
	}

	probeSink.Store(uint32(probe[StackReserve-1]))
}
```

Details that matter:

- The function is **`//go:noinline`**. Inlining it would move the array into the
  caller's frame, where it would no longer force a stack growth on first use.
- It **touches one byte per page**, with each write feeding the next, so neither
  the array nor the frame can be optimised away.
- `probeSink` is written with an **atomic store**, because `ProbeStack` runs on
  every goroutine that calls a foreign function, at the same time.
- After the first call on a goroutine the stack has already grown, so the probe
  is a frame adjustment plus one store per page — cheap enough to put in front
  of every foreign call, which is what generated wrappers do.

`StackReserve` is `32 << 10` — 32 KiB. It has to cover the deepest foreign frame
a bound function can create, since nothing can grow the stack for it. 32 KiB is
larger than any sane generated shim uses and small enough that the probe does
not cost a noticeable copy.

## What is guaranteed

- Passing **GC torture**: `GOGC=1` plus `runtime.GC()` in a loop under 32
  goroutines.
- Passing **`SIGPROF`/`SIGURG` torture**: profiling and preemption signals
  arriving during calls.
- Passing **`-race`** with 32+ goroutines hammering calls, and **`GOMAXPROCS=1`**
  (where there is no other `P` to absorb a delayed cycle, so the delay is
  visible).
- **No pointer retention is required by the runtime.** Borrowed slices and
  strings stay alive across the call because the wrapper calls `KeepAlive`
  after the trampoline, not before it.

## What is not guaranteed

**The `P` is not released during the call.** This is the documented trade-off,
not a bug: a foreign call in flight delays a GC cycle by up to its duration.
Measured, a 2.0 ms call delayed the worst GC cycle by 1.1 ms, versus 2.4 ms for
a spinning Go goroutine. For short, compute-bound calls this is the right trade.
For long or blocking work it is the wrong one — move that work to another
process.

**No callbacks from C++ into Go.** There is no reverse trampoline in v1. Code
that needs to be called back must be restructured: batch the work, or return a
description of the work and let Go dispatch it.

**No exceptions across the boundary.** The generated shim catches
`std::exception` and `...` and converts them. An exception that escaped the shim
would unwind into a Go frame, which is not something the runtime survives, so
the shim is the guarantee — not the compiler.

**No cancellation and no timeout.** A foreign call runs to completion. There is
no context to thread into it and no way to interrupt it from Go, because
interrupting it would mean putting a signal into foreign frames.

**Shims must stay inside the reserve.** Deep recursion, large stack arrays, or
`alloca` in the shim are out of contract. 32 KiB of C++ stack is a lot for a
shim and very little for a parser.

**Closing a library is not coordinated with calls in flight.** `Library` is safe
for concurrent use — symbols can be resolved from many goroutines, and `Close`
is serialised against them — but keep a library open for as long as any call
into it can be running.

## Writing a shim that stays inside the contract

- Do not store pointers to Go memory. Copy what you need to keep.
- Do not call back into Go, directly or through a callback you were handed.
- Keep the C++ side shallow: no recursion proportional to input size, no large
  stack buffers.
- Report failure by throwing, and let the shim convert it — do not abort.
- Return promptly. If the call can block on I/O or a lock, it belongs in
  another process.
