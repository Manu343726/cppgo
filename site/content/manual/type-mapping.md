+++
title = "Type mapping"
description = "How every C++ type crosses to Go: scalars and widths, pointer and length pairs, mirrored records, opaque values, standard-library types, callbacks, and the types the boundary refuses."
weight = 40
+++

**Audience:** you are reading a generated signature and want to know what
crosses, or you are deciding whether a C++ type can cross at all.

## The table

You do not write the Go type: cppgo maps the C++ declaration and the generated
signature is the answer. The table is how to read it. **Measured** means an
end-to-end test in this repository exercises it against a real library;
**designed** means the mechanism is specified and the row is not yet proven end
to end.

| C++ | Go | notes |
|---|---|---|
| `bool` | `bool` | one byte on the wire; the trampoline passes `0` or `1` |
| `char`, `signed char` | `int8` | plain `char` is signed on the supported targets |
| `unsigned char` | `uint8` | |
| `short`, `unsigned short` | `int16`, `uint16` | |
| `int`, `unsigned int` | `int32`, `uint32` | |
| `long`, `long long`, `unsigned long`, `unsigned long long` | `int64`, `uint64` | LP64, validated against libclang |
| `int8_t` … `uint64_t` | the same width | fixed-width aliases resolve by size |
| `size_t`, `uintptr_t`, `intptr_t`, `ptrdiff_t` | `uint64`, `int64` | validated |
| `float`, `double` | `float32`, `float64` | |
| `wchar_t`, `char16_t`, `char32_t` | `int32`, `uint16`, `uint32` | validated by size |
| enum (scoped or unscoped) | — | **refused today**: the whole header is a generation error until enums are emitted; pass the value as its underlying integer |
| `T *`, `T &` | `*T` | borrowed for the call |
| `(T *p, size_t n)` | `[]T` | borrowed; `const` decides whether the callee may write |
| `(char *p, size_t n)` | `string` | borrowed; **not NUL-terminated** |
| mirrorable record, by value | `T` | the Go struct *is* the C layout; measured (`Point`, 16 bytes) |
| other record, by value | `*T` | the shim copies in and out; measured (`Sample`, 24 bytes) |
| `T[N]` (record field) | `[N]T` | fixed-size array |
| `std::string` | `string` | copied in and out; measured |
| `std::vector<T>` | `[]T` | copied in and out; measured |
| `std::string_view` | `string` | borrowed for the call; **designed, not measured** |
| `std::span<T>` | `[]T` | borrowed; **designed, not measured** |
| `std::array<T,N>` | `[N]T` | by value through the shim; **designed, not measured** |
| `std::optional<T>` as a parameter | `*T` | nil is `nullopt`; **designed, not measured** |
| `std::optional<T>` as a return | `(T, bool)` | absence is not a failure; **designed, not measured** |
| `std::unique_ptr<T>` | `*T` | a parameter consumes, a result owns; **designed, not measured** |
| `std::shared_ptr<T>` | `*T` | mapped, but **not supported yet** |
| function pointer `R (*)(Args...)` | `func(Args...) R` | the callback table's address; measured |
| `std::function<R(Args...)>` | `func(Args...) R` | the shim constructs it from the Go function; measured |

`long double` and `__int128` have no Go value type. A signature that uses one is
a generation error naming the parameter and the fix: widen or narrow it at the
C++ boundary to `double`/`int64`, or wrap it.

## Pointers and lengths become slices

A pointer parameter followed by an integer length parameter becomes a slice: the
pointer is the data, the length is `len(slice)`, and the slice is borrowed for
the call.

```cpp
double dot(const double *ys, std::size_t n) const;
```

```go
func (v *Vector) Dot(ys []float64) (float64, error)
```

The C++ side must not keep the pointer after it returns. Go may reuse that
memory as soon as the call is over, and the wrapper's `KeepAlive` only keeps it
reachable for the duration of the call.

One length may serve several pointers; the generated binding checks before the
call that every slice sharing a length has the same length, and returns an error
if not:

```
cppgo: mathx::saxpy: the slices must have the same length, got 3 and 4
```

A Go `string` crosses the same way: a pointer and a length, no copy and no NUL
terminator. A C++ function that wants a C string needs the terminator in a
`[]byte` you own.

## Mirrorable records are Go structs

A record whose fields are all public, whose layout is the natural Go layout, and
which has no bitfield, reference, pointer, union member or non-mirrorable field
is **mirrored**: the generated Go struct has the same size, alignment and field
offsets, and the generated file asserts all three at compile time.

```go
// mathx::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.
type Point struct {
	X float64
	Y float64
}
```

Everything else is an **opaque value type**: a Go struct with unexported
storage, `Close()` from the destructor, `Clone()` from the copy constructor when
one exists, and generated accessors for every public field. A class with private
members is opaque; that is the normal case, not a failure.

## Callbacks

A function pointer or a `std::function` parameter becomes a Go `func`, and the
binding uses the **reentrant carrier**:

```cpp
std::int32_t apply(const std::function<std::int32_t(std::int32_t)> &fn,
                   std::int32_t value) const;
```

```go
func (s *Signal) Apply(fn func(int32) int32, value int32) (int32, error)
```

The limits are the ABI's, not a policy: at most six integer and eight
floating-point arguments, and one register result. A callback that takes a
stack-passed argument is a generation error naming the sixth argument.

## What the boundary refuses

These are stated as one-line generation errors, naming the declaration and the
fix:

- **enums**, nested types, inheritance, static data members, member function
  templates, operators and conversion operators, variadic functions — constructs
  the emitter does not emit yet;
- a **class template** with no `-instantiate` entry;
- a macro that is the whole API;
- `long double` / `__int128`;
- an incomplete type used where a value is needed;
- a callback taking more than six integer or eight floating-point arguments.

The full list, with the exact messages, is in
[validation]({{< ref "manual/validation.md" >}}).

Next: [how arguments are assigned to registers]({{< ref
"manual/calling-convention.md" >}}).
