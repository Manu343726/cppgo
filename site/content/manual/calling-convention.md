+++
title = "Register assignment"
description = "How cppgo lays arguments out in RDI/RSI/RDX/RCX/R8/R9 and XMM0-XMM7, where this and the hidden sret pointer sit, how results come back, and why the trampoline switches the stack pointer."
weight = 50
+++

cppgo's generated code and its generated trampolines come from the same tables,
so the convention is cppgo's own — modelled on the System V AMD64 ABI. Both
sides of the boundary are written by the same emitter, which is why the rules
can be this small.

## The registers

Arguments are assigned in declaration order, independently per class:

| class | registers |
|---|---|
| integers and pointers | `RDI`, `RSI`, `RDX`, `RCX`, `R8`, `R9` |
| floats (`float`, `double`) | `XMM0` … `XMM7` |

A float argument does not consume an integer slot, and an integer argument does
not consume an XMM slot. The only thing that expands is a Go `string` or slice,
which is **two integer arguments**: the pointer, then the length.

```
func Dot(a, b []float64) (float64, error)
```

becomes four integer slots plus one float result:

| slot | register | value |
|---|---|---|
| `fn` | `R12` | resolved symbol address (not a C argument) |
| `a` | `RDI` | pointer to the first element |
| `a_len` | `RSI` | `len(a)` |
| `b` | `RDX` | pointer |
| `b_len` | `RCX` | length |
| result | `XMM0` | the `double` |

## Member functions: `this` is an integer argument

A non-static member function carries an implicit `this`, which takes the first
available **integer** slot:

```
_ZNK5mathx6Vector3dotEPKdm      mathx::Vector::dot(const double *, size_t) const
```

| slot | register |
|---|---|
| `this` | `RDI` |
| `a` (the `double *`) | `RSI` |
| `a_len` (`size_t`) | `RDX` |
| result | `XMM0` |

That is exactly what the Itanium ABI specifies, and it is why a `const` method
needs no special handling: `const` changes the mangling (`K`), not the call. A
constructor also takes `this` first; its float arguments then start at `XMM0`,
so `Vector(double, double)` is `this=RDI`, `x=XMM0`, `y=XMM1`.

Static member functions and free functions have no `this`, so their first
argument is `RDI`.

### The hidden sret pointer

When a class is returned by value, the Itanium ABI passes a destination pointer
as a hidden first argument. Direct-ABI v1 **reserves** that slot in the layout
so `this` still lands where the ABI expects it, but the binding itself is
rejected: nothing writes the destination address. The slot exists for the
layout's sake, not for a call you can make. Use the shim mode for by-value
returns.

## Results

| result | register |
|---|---|
| integer, pointer, `cppgo.Handle` | `RAX` |
| `float32` / `float64` | `XMM0` |
| `bool` | low byte of `RAX` (the wrapper tests `& 0xff`) |
| `int32` / status | low 32 bits of `RAX` (`MOVL`) |
| `void` | nothing |

A function returns at most one value in a register. A trailing `error` is not a
second return value: in shim mode it is the `int32_t` status, and a value is
written through an out-pointer the wrapper allocates. In direct ABI mode a
trailing error adds no channel at all, so it is always `nil` unless the C++
function returns an integer status — see [error
handling]({{< ref "manual/errors.md" >}}).

## Register exhaustion is a build error

There are six integer slots and eight XMM slots. A binding that needs more is
refused, with the count it needed:

```
cppgo gen: ./bindings/mathx/mathx.go:42: TooMany: function TooMany needs 7
integer argument registers, but the SysV ABI only provides 6 (RDI, RSI, RDX,
RCX, R8, R9); group or reduce the arguments
```

cppgo does not spill the seventh argument to the stack: the generated
trampoline has no stack-argument path, and silently emitting one would be a bug
that only appears on some signatures. Group the values in a struct passed by
pointer, or take a slice.

## The trampoline

Here is a complete generated trampoline, for a free function taking two
`double`s:

```asm
// func cppgo__ZN5mathx3addEdd(fn uintptr, a0 float64, a1 float64) (ret float64)
TEXT ·cppgo__ZN5mathx3addEdd(SB), NOSPLIT, $0-32
	MOVQ	fn+0(FP), R12
	MOVSD	a0+8(FP), X0
	MOVSD	a1+16(FP), X1
	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+24(FP)
	RET
```

Read it in four steps:

1. **Load the arguments from the Go frame.** `fn` is passed as an ordinary Go
   argument and kept in `R12`; the C arguments go straight into their
   registers. This happens *before* `SP` moves, because `FP` is relative to
   `SP`.
2. **Save and align the stack.** `R13` keeps the Go stack pointer; `ANDQ $~15`
   aligns `RSP` to 16 bytes, which SysV requires *before* the `CALL` pushes the
   return address.
3. **Call.** `CALL R12`. The C++ frame lives on the goroutine's stack from here
   on, which is why the wrapper calls `ProbeStack` first and why `StackReserve`
   is 32 KiB.
4. **Restore, then store.** `MOVQ R13, SP` puts the Go stack pointer back, and
   only then is the result written to `ret+24(FP)`; storing before the restore
   would write to the wrong address.

`R12` and `R13` are callee-saved in SysV, so the C++ function preserves them
across the call without cppgo doing anything.

The function is `NOSPLIT` with a `$0` frame: it allocates nothing, touches no Go
memory, and cannot be asynchronously preempted. That is the property the design
relies on — `runtime.isAsyncSafePoint` returns false for any assembly function,
and a foreign stack pointer is never inside `g.stack`, so the GC skips the
goroutine while the call is in flight.

## What the caller pays

`ProbeStack` runs before every call. It guarantees at least `StackReserve`
(32 KiB) of headroom below the current frame, by entering an assembly function
with a 32 KiB frame — not by allocating a large Go array, which the compiler
would zero on entry (~1.2 µs per call, measured). After the first call on a
goroutine it is a frame adjustment, and it measured 14.9 ns versus 13.9 ns for
the bare trampoline on the machine described in [benchmarks]({{< ref
"project/benchmarks.md" >}}).

The other cost is that the `P` stays with the goroutine: the runtime does not
hand it off, so a foreign call in flight delays a GC cycle by up to its
duration. That is the deliberate trade for not paying cgo's 100-cycle entry.
