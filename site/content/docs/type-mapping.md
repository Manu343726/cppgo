---
title: "Type mapping"
description: "The complete Go to C type table, how numbers, strings, slices, structs and handles cross the boundary, and how errors are signalled."
weight: 50
---

The generator's type table is the contract between the Go signature you write
and the C++ declaration it produces. It is deliberately small: every row below
is something cppgo can represent without guessing, and everything not in the
table is something that cannot cross.

## The table

| Go | C | notes |
|---|---|---|
| `int8`, `int16`, `int32`, `int64` | same width | signed, exact |
| `uint8`, `uint16`, `uint32`, `uint64` | same width | unsigned, exact |
| `int` | `int64_t` | platform width is not exposed, so the wire type is fixed |
| `uint` | `uint64_t` | same |
| `uintptr` | `uint64_t` | same |
| `float32` | `float` | travels in `XMM` registers |
| `float64` | `double` | travels in `XMM` registers |
| `bool` | `int32_t` | `0` / `1`; a bool is not one byte on the wire |
| `string` | `const char *, size_t` | pointer then length, **not** NUL-terminated |
| `[]T` (T numeric) | `T *, size_t` | pointer then length; caller owns; callee must not retain |
| `*T` (`T` is `//cppgo:repr(C)`) | `T *` | structs cross only by pointer |
| `Handle` | `void *` | an opaque C++ object |
| `error` (second result) | `int32_t` + `cppgo_last_error` | always the last result |

## What each row means in practice

**Fixed widths are fixed.** `int` becomes `int64_t`, not `int` or `long`. That
is intentional: a Go `int` that silently changed width on the C++ side would
make a binding's behaviour depend on the target, and the point of a generated
contract is that it does not.

**`bool` widens.** The wire type is `int32_t` with `0` for false and `1` for
true, because the C++ side should not depend on `_Bool` or `bool` having a
particular size across compilers and ABIs.

**Strings and slices are two registers each.** `(pointer, length)` in that
order, length as `size_t`. The pointer may be `nil` when the length is zero —
`Ptr` and `StrPtr` both return `nil` for an empty value, so a shim can treat
`nullptr` and `0` as the same case.

**Structs never travel by value.** Only `*T` with `//cppgo:repr(C)` on the
type. This keeps layout knowledge in C++ and keeps the generated Go free of
padding arithmetic.

**`Handle` is `void *`.** It is how a C++ object survives between calls without
its layout ever being known to Go — see
[stateful objects](/tutorials/stateful-objects/). The pointer is not a Go
pointer, so the GC does not trace it and will not move it; it is your job to
destroy the object with an explicit call.

**`error` is always last.** A function may return a value *and* an error, but
the error is the second result, and on the wire it becomes the `int32_t` return
status plus the thread-local message. A plain value-only function returns its
value directly in `RAX` or `XMM0` and has no status.

## Ownership rules that follow from the table

- Slices and strings **borrow** Go memory. The callee reads them during the
  call and must not store the pointer. The wrapper keeps the value alive across
  the call with `KeepAlive`, so the GC will not collect or move it mid-call.
- A `Handle` is the opposite: it points at memory C++ allocated, and Go only
  carries the number. Nothing in Go will free it.
- A returned `error` is a copied Go string by the time you see it. The
  original C string is thread-local and dies at the next call.

## What is not in the table

Maps, channels, funcs, interfaces, and non-`repr(C)` structs have no mapping,
so they cannot appear in a bound signature. Neither can a by-value struct, a
variadic parameter, or a generic type parameter. If a C++ API needs one of
these, the boundary is the wrong shape: flatten it into a pointer plus a
length, a handle, or several calls, and keep the composite type on one side.

## Worked conversions

```go
//cppgo:external score
func Score(name string, weights []float64, strict bool) (int64, error)
```

```c
int64_t score(const char *name, size_t name_len,
              const double *weights, size_t weights_len,
              int32_t strict);
```

Three arguments become six registers — `RDI`/`RSI` for the name,
`RDX`/`RCX` for the weights, `R8` for `strict` — and the result is `RAX`. A
non-zero `RAX` from a function declared to return `(int64, error)` is a status,
not a value: the wrapper reads `cppgo_last_error`, builds the error, and the
value is discarded.
