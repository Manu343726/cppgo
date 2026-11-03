---
title: "ABI contract"
description: "Register assignment, argument order for slices and strings, struct passing, and the thread-local error convention — both sides are generated, so cppgo owns this."
weight: 40
---

Both sides of the boundary are generated, which means cppgo is not constrained
by an existing convention — it defines one. This page is that definition. If
you write a shim by hand, or read generated assembly, this is the contract you
must satisfy.

## Register assignment

Integer and pointer arguments fill the System V integer registers in
declaration order:

| argument | register |
|---|---|
| 1st | `RDI` |
| 2nd | `RSI` |
| 3rd | `RDX` |
| 4th | `RCX` |
| 5th | `R8` |
| 6th | `R9` |

Floating point arguments fill the SSE registers in declaration order:
`XMM0`, `XMM1`, … `XMM7`. The two banks are filled independently, exactly as
System V specifies, so a signature like `(int64, float64, int64)` puts the
integers in `RDI` and `RSI` and the float in `XMM0`.

Results come back in two fixed places:

| result kind | register |
|---|---|
| integer / pointer | `RAX` |
| floating point | `XMM0` |

The runtime trampoline copies both out after the call regardless of which one
the signature declares, and the Go side reads whichever its signature says.
When a signature has both (it should not), the integer result is the `RAX`
value.

## Argument shapes

Slices and strings do not travel as a Go header. They cross as a
**(pointer, length) pair, pointer first**, which is why a `[]float64` argument
occupies two registers and a `func Dot(a, b []float64)` consumes four:

```
RDI = a.ptr    RSI = len(a)
RDX = b.ptr    RCX = len(b)
```

The length is a `size_t`. An empty slice is `(nil, 0)` — the wrapper's `Ptr`
helper returns `nil` for an empty slice, so a shim that checks the pointer
before dereferencing gets the right answer for free.

Nothing is NUL-terminated. A `string` becomes `(pointer, length)` and no
terminator is appended, because the length already says where it ends. A C++
function that genuinely needs a C string must be handed a `[]byte` with an
explicit `\0` in it.

## Structs

Structs cross **only by pointer**, and only when the struct is declared
`//cppgo:repr(C)`. There is no by-value struct passing in v1: a by-value struct
would need the generator to reconstruct layout rules on the Go side, and the
layout already exists in C++.

Passing a pointer means the same rule as everything else that borrows Go memory:
the callee may not keep the pointer after the call returns.

## The error convention

A C++ function that can fail returns `int32_t`. `0` means success. Anything
non-zero means failure, and the shim has stored a message that Go reads with:

```c
const char *cppgo_last_error(void);
```

The accessor is **thread-local and valid only until the next call into the
library**. That is why `cppgo.LastError` copies the bytes into a Go string
immediately rather than handing back a pointer: the next call — from any
goroutine on the same thread — overwrites the buffer.

The generated Go wrapper turns a non-zero status into an error of the form:

```
sym: message
```

so a `Dot` that rejects mismatched lengths arrives in Go as
`dot: length mismatch`, naming the symbol as well as the message. The message
text is exactly what the C++ side stored; `NativeError` does not decorate it,
and the wrapper adds the symbol prefix on top.

`cppgo_last_error` is **optional**. A library that cannot fail does not need to
export it, and `cppgo.LastError` returns `nil` when the symbol is absent or
when the string it returns is empty.

## Exceptions

Nothing unwinds across the boundary. The generated shim wraps each foreign
function, catches `std::exception` (message from `what()`) and `...` (a generic
message), stores the text, and returns non-zero. A `throw` inside your C++ is
therefore a normal Go error, not a crash — and a `throw` can never land in a Go
frame, which the runtime would not survive.

## Alignment

`RSP` is 16-byte aligned immediately before `CALL`, as System V requires.
`Args` is 112 bytes, 8-byte aligned, with no padding; the integer bank occupies
offsets 0–40 and the float bank offsets 48–104. The offsets are asserted by a
test, so a layout change fails loudly instead of calling into the void.

## What the callee may not do

Three prohibitions, all consequences of borrowing rather than copying:

1. **Keep a pointer to Go memory after the call returns.** Slices, strings and
   `repr(C)` structs all borrow; retaining them is a use-after-free, and the GC
   has no way to know about the reference.
2. **Call back into Go.** Not supported in v1 — there is no trampoline in that
   direction, and the goroutine's stack is not in a state that could accept one.
   Batch instead: one call that processes 10 000 rows, rather than 10 000 calls.
3. **Assume the goroutine's stack is available.** Foreign frames live in the
   `cppgo.StackReserve` headroom (32 KiB by default) below the caller, not on a
   stack that can grow on demand. Deep recursion or large stack arrays in the
   shim are out of contract.
