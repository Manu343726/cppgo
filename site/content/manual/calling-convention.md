+++
title = "Register assignment"
description = "How the generated trampolines lay arguments out in RDI/RSI/RDX/RCX/R8/R9 and XMM0-XMM7, where this and the hidden sret pointer sit, how a thunk returns a value and a status, and what the caller pays before each call."
weight = 50
+++

**Audience:** you are reading a generated trampoline, debugging a calling
convention, or checking why a signature needs a thunk.

cppgo's generated code and its generated trampolines come from the same tables,
so the convention is cppgo's own, modelled on the System V AMD64 ABI (the
agreement Linux/x86-64 uses for C). Both sides of the boundary are written by the
same emitter, which is why the rules can be this small.

## The registers

Arguments are assigned in declaration order, independently per class:

| class | registers |
|---|---|
| integers and pointers | `RDI`, `RSI`, `RDX`, `RCX`, `R8`, `R9` |
| floats (`float`, `double`) | `XMM0` … `XMM7` |

A float argument takes an XMM slot, and an integer argument takes an integer slot. The two banks are counted independently. A Go `string` or slice is **two integer arguments**:
the pointer, then the length.

```
func (v *Vector) Dot(ys []float64) (float64, error)
```

becomes:

| slot | register | value |
|---|---|---|
| `this` | `RDI` | the object address |
| `ys` | `RSI` | pointer to the first element |
| `ys_len` | `RDX` | `len(ys)` |
| out | `RCX` | where the thunk writes the `double` |
| status | `RAX` | 0 on success, non-zero on a caught exception |

The last two exist because `dot` omits `noexcept`. The call goes through a thunk, and the thunk returns a status and writes the value through the out-pointer.

## Member functions: `this` is an integer argument

A non-static member function carries an implicit `this`, which takes the first
available **integer** slot:

```
_ZNK5mathx6Vector3dotEPKdm      mathx::Vector::dot(const double *, size_t) const
```

| slot | register |
|---|---|
| `this` | `RDI` |
| `ys` (the `double *`) | `RSI` |
| `ys_len` (`size_t`) | `RDX` |
| out | `RCX` |
| status | `RAX` |

That is what the Itanium C++ ABI specifies, and it is why a `const` method needs
no special handling: `const` changes the mangling (`K`), not the call. A
constructor also takes `this` first. Its float arguments then start at `XMM0`,
so `Vector(double, double)` is `this=RDI`, `x=XMM0`, `y=XMM1`.

Static member functions and free functions have no `this`. Their first argument is `RDI`.

### The hidden `sret` pointer

When a record is returned by value, the Itanium ABI passes a destination pointer
as a hidden first argument (`sret`, "structure return"). The generated thunk
passes the address of storage the Go side owns and constructs the result there. The value never has to fit in one register. This is one of the reasons a
by-value return is a thunk rather than a direct call.

## Results

A direct call returns in one register:

| result | register |
|---|---|
| integer, pointer | `RAX` |
| `float32` / `float64` | `XMM0` |
| `bool` | low byte of `RAX` |
| a 16-byte POD | up to two registers |
| `void` | nothing |

A thunk returns `int32_t` in `EAX`: **0 means success**, non-zero means a C++
exception was caught, and its message is waiting in a thread-local string. The
value, when there is one, is written through a trailing out-pointer. That is why
a non-`noexcept` function's Go signature is `(T, error)` and a `noexcept` one's
is just `T`.

## Register exhaustion is a thunk, not an error

There are six integer slots and eight XMM slots. A direct binding exists only for
a signature that fits them. Everything else is a thunk, and the thunk's declared
shape is an argument block when the registers run out:

```
more than 6 INTEGER or 8 SSE arguments  →  thunk takes a pointer to a block and unpacks it
```

That row is **designed, not measured**: no fixture in this repository has a
signature that long yet. What is measured is the rule that decides it, the direct carrier is never chosen for an argument list that overflows the register file.

## The trampoline

Two real generated trampolines. The first is the direct carrier for
`mathx::Vector::scale(double) noexcept`, a function with no result:

```asm
// func cppgoCall_5e3a9d471306(fn uintptr, this uintptr, a1 float64)
TEXT ·cppgoCall_5e3a9d471306(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVSD	a1+16(FP), X0
	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP
	RET
```

The second is the thunk carrier for the constructor, which returns a status:

```asm
// func cppgoCall_7d8c482bce3e(fn uintptr, this uintptr, a1 float64, a2 float64) int32
TEXT ·cppgoCall_7d8c482bce3e(SB), NOSPLIT, $0-36
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVSD	a1+16(FP), X0
	MOVSD	a2+24(FP), X1
	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP
	MOVL	AX, ret+32(FP)
	RET
```

Read either in two parts. What belongs on this page is where the values come
from:

1. **`fn` is a Go argument.** It is an ordinary Go argument, read from the Go
   frame into `R12`, and `CALL R12` is what reaches the symbol.
2. **The C arguments are loaded into their registers before `SP` moves**, because
   `FP` is relative to `SP`.
3. **The result is stored after `SP` is restored**, for the same reason.

The rest, the  `SP` switch, the 16-byte alignment SysV requires before the
`CALL`, and what `NOSPLIT` buys, is explained once in [How it
works]({{< ref "project/architecture.md" >}}).

`R12` and `R13` are callee-saved in SysV. The C++ function preserves them across the call without cppgo doing anything.

## What the caller pays

`ProbeStack` runs before every call and guarantees `StackReserve` (32 KiB) of
headroom. The mechanism, and why the probe is assembly instead of a large Go local array, is in [How it
works]({{< ref "project/architecture.md" >}}). It measured 14.9 ns against
13.9 ns for the bare trampoline, at the clock [benchmarks]({{< ref
"project/benchmarks.md" >}}) describes.

The other cost is that the `P` stays with the goroutine on the direct and thunk
carriers. The runtime keeps the `P` with the goroutine. A foreign call in flight therefore delays a GC cycle by up to its duration. The reentrant carrier enters through
`runtime.cgocall` and pays the runtime's handshake instead. That is what makes a
callback legal, and it costs about ten times a direct call.
