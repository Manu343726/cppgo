+++
title = "Type mapping"
description = "How every supported Go type crosses to C and C++: widths, pointer and length pairs, booleans, enums, handles, and the types the boundary refuses."
weight = 40
+++

## The table

The Go type decides the register class and the C spelling. Widths are the part
that bites: the generator compares `sizeof` of the C++ type with the width of
the Go type and refuses a mismatch rather than passing a truncated value.

| Go type | C type (shim header) | width | notes |
|---|---|---|---|
| `int8` | `int8_t` | 1 | signed |
| `int16` | `int16_t` | 2 | |
| `int32`, `rune` | `int32_t` | 4 | |
| `int64` | `int64_t` | 8 | |
| `int` | `int64_t` | 8 | Go `int` is 64-bit here |
| `uint8`, `byte` | `uint8_t` | 1 | unsigned |
| `uint16` | `uint16_t` | 2 | |
| `uint32` | `uint32_t` | 4 | |
| `uint64` | `uint64_t` | 8 | |
| `uint` | `uint64_t` | 8 | |
| `uintptr` | `uint64_t` | 8 | |
| `float32` | `float` | 4 | XMM register |
| `float64` | `double` | 8 | XMM register |
| `bool` | `int32_t` | 1 | 0 or 1. In ABI mode the C++ type must be `bool` |
| `string` | `const char *`, `size_t` | — | pointer first, length second. Not NUL-terminated |
| `[]T` (T numeric) | `T *`, `size_t` | — | pointer first, length second. Caller owns the buffer |
| `*T` | `T *` | — | `T` is a builtin or a type of the bound package |
| `cppgo.Handle` | `void *` | — | opaque foreign object |
| a `//cppgo:class` type | `this` pointer | — | the receiver; not a normal argument |
| `error` (last result) | `int32_t` | 4 | status convention, shim mode |

Two rows deserve their own explanation.

### Strings and slices are (pointer, length)

A Go `string` crosses as `const char *` followed by a `size_t`. cppgo does not
append a NUL, because that would copy the string: `StrPtr` returns a pointer
into the string's own bytes, and the length is passed beside it. A C++ function
that wants a C string needs the NUL in a `[]byte` you own.

A `[]T` crosses the same way: `T *` and `size_t`, pointer first. `Ptr` returns
`nil` for an empty slice, and the length is `0`, so an empty slice is never a
dangling pointer. The callee must not keep the pointer after it returns: Go may
reuse that memory as soon as the call is over.

### `bool` is a C++ `bool` in ABI mode

In shim mode the C++ side sees `int32_t` and tests it against zero. In the direct
ABI mode there is no shim to convert, so the Go `bool` is passed as the integer
`0` or `1` in a register and the C++ declaration must be `bool` — the generator
checks libclang's type kind, not just the width, because `int` has the same
width and a different meaning.

## Widths must match exactly

The generator compares the Go type's width with `sizeof` of the C++ parameter:

| C++ (LP64) | size | bind it as |
|---|---|---|
| `bool` | 1 | `bool` |
| `char`, `int8_t` | 1 | `int8` / `uint8` |
| `short` | 2 | `int16` / `uint16` |
| `int` | 4 | `int32` / `uint32` |
| `long`, `long long`, `size_t`, `std::size_t` | 8 | `int64` / `uint64` / `int` / `uint` |
| `float` | 4 | `float32` |
| `double` | 8 | `float64` |
| `T *`, `T &` | 8 | `*T` or `cppgo.Handle` |
| enum | its underlying type | `int32` (accepted when the enum is 4 bytes or narrower) |

A Go `int` is 8 bytes, so it binds a C++ `long`, never a C++ `int`. This is the
most common first error, and the message says both widths:

```
parameter 2 (n int): the Go type is int (8-byte) but the C++ type is "int"
(4-byte): the two must have the same width, because the value is passed in one
register
```

## What the boundary refuses

These are rejected when the header is available, which is the normal case:

- **`std::` types by value** — `std::string`, `std::vector`, `std::span`.
  Their ABI and lifetime belong to the shim mode. A pointer or reference to one
  is still a pointer, so `const std::string &` is a pointer slot; whether that
  is a good idea is a separate question.
- **Structs or classes passed by value** — the SysV ABI classifies them into
  registers or memory, and the generated trampoline does not implement the
  aggregate rules. Take them by `const &` or use shim mode.
- **Structs or classes returned by value** — the Itanium ABI brings them back
  through a hidden sret pointer, which direct-ABI v1 reserves but does not
  emit. Use shim mode.
- **Virtual functions** — the call has to walk the vtable; a fixed symbol can
  only reach one override. Use shim mode.
- **Inline / header-only functions** — no dynamic symbol exists. Move the
  definition out of line.
- **Templates that were never instantiated** — no mangled name. An explicit
  instantiation produces an ordinary symbol and can be bound.
- **Arrays, function pointers, pointers to members** — libclang reports a type
  the ABI mode does not classify. Shim mode.
- **`[]T` of a named type, `*[]T`, `*error`, `*string`** — no C representation.
- **A class handle as a parameter** — a `Vector` value is already the object
  pointer; passing it as an argument is refused. Use `cppgo.Handle` if the C++
  side takes an opaque pointer.
- **Returning a `string` or a slice** — the C++ side cannot allocate Go memory.
  Fill a buffer the caller passes in.

## Pointers to your own types

`*T` accepts a builtin type or a type of the bound package:

```go
//cppgo:external move
func Move(p *Point, dx, dy float64)
```

In shim mode the generated `cppgo_shim.h` forward-declares `struct Point;` as an
opaque type; the C++ side has to include the real definition or the generated
line has to be replaced. Passing a pointer to a Go struct whose layout mirrors a
C struct is the caller's responsibility: the generator does not verify the
layout.

Next: [how arguments are assigned to registers]({{< ref
"manual/calling-convention.md" >}}).
