---
title: "Report failures from C++ to Go"
description: "Exceptions and status codes today; the full projection as designed."
weight: 50
---

A C++ library reports failure in more than one way: it throws, it returns a
`std::error_code`, a status integer, an `std::optional`, a project-specific
result type. A Go caller should not have to know which one it is looking at.
This guide covers what the generated package does today, and marks the part that
is designed and not shipped.

The rule that shapes every generated signature:

> **A call that can fail returns `(T, error)`, the value first and the error
> last. A call that cannot fail returns no error at all.**

There is no error value that is always nil, and a failure is never turned into a
zero result silently.

## Exceptions, today

A C++ function or method that can throw becomes a Go call that returns an error.
When it fails, the error is a `*cppgo.CallError` naming the C++ declaration,
wrapping a `*cppgo.NativeError` that carries the library's message:

```console
At(2): cppgo: mathx::Vector::at: Vector::at: index out of range
  is a *cppgo.CallError: true
```

Both types have `Unwrap`, so `errors.As` finds either one:

```go
var call *cppgo.CallError
if errors.As(err, &call) {
	fmt.Println("declaration:", call.Symbol)   // mathx::Vector::at
}

var native *cppgo.NativeError
if errors.As(err, &native) {
	fmt.Println("library said:", native.Message) // Vector::at: index out of range
}
```

`errors.Is` works for the sentinel `cppgo.ErrClosed`, which is what a method on a
nil or closed receiver returns. A `CallError` has no comparable identity, so
`errors.Is` on one answers false; use `errors.As`.

## A status code, today

Some C libraries report a class of failures as a 32-bit return code and put the
value in an out-parameter. Name that declaration with `-status`. This is the
declaration, from
[`internal/gen/testdata/status/status.hpp`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/status/status.hpp):

```cpp
std::int32_t parse(const std::string &text);
```

```console
$ cppgo gen -header include/status.hpp -package status \
            -library build/libstatus.so -status 'status::parse' -out ./status
```

The generated Go signature drops the integer and returns only the error:

```go
// Parse calls status::parse.
func Parse(text string) error
```

Any non-zero return is the failure.

`-status` refuses two entries, because guessing would be worse than failing: a
name that matches no declaration (`it names nothing this header declares`), and
a declaration whose return is a value rather than an integer (`and a status is a
32-bit integer`). A function that returns `double` and takes an error
out-parameter is the `out_param` convention below, not this one.

## What a full projection looks like (designed)

The design maps eight conventions to those same two Go shapes. Only the exception
and status rows work today.

| C++ convention | Go result today | Go result, designed |
|---|---|---|
| exceptions (`std::exception`, `std::system_error`, …) | `(T, error)`, `*cppgo.CallError` wrapping `*cppgo.NativeError` text | same, with the system error's code and category when it has them |
| `-status` on a 32-bit return | `(T, error)` / `error`, non-zero is failure | same |
| `std::error_code` | **generation error**: `"std::error_code" is a standard-library type outside the emitted table` | `(T, error)` with a code, a category and a message |
| `std::expected<T,E>`, `tl::expected` | not classified as an error | `(T, error)`; `E` projected |
| `std::optional<T>` as a result | `(*T, error)`; nil means absent, and absent is **not** an error | unchanged, or `(T, error)` when a package config names the message for absence |
| `errno` | not read | `(T, error)` with category `errno` |
| `bool` + out-parameters | `(bool, error)`; the `bool` is a value | `(T, error)`; the config names the value and error channels |
| a custom result type (`Result<T>`, `absl::StatusOr<T>`) | not inferred | `(T, error)` when its shape is `has_value`/`value`/`error` |
| `std::variant<T,E>` with a known error alternative | not inferred | `(T, error)`; the config names the alternative, never guessed |

Designed, not shipped. Until it lands, a library that returns `std::error_code`
is refused at generation time, and the message says what to do: cross the failure
as a scalar or a wrapper with a supported signature, or use `-status` when the
convention is an integer.

## When your C++ cannot be bound safely

**A C++ exception that nobody catches terminates the process.** cppgo catches the
exceptions it can see, but it can only do that when the header that declares the
throwing function can be included by the C++ side cppgo builds.

The practical consequence: if your library's API throws and its headers cannot be
included that way (a private implementation, a macro-only API), that call cannot
be bound safely. The fix belongs in the library — a `noexcept` entry point that
returns a status — not in the Go side. The measurements behind this are in
[Limits]({{< ref "explanation/limits.md" >}}#hard-limits) and
[Architecture]({{< ref "explanation/architecture.md" >}}#exceptions-across-the-boundary).

## Callbacks and panics

A Go function passed to C++ may panic. The panic does not unwind into C++: it is
captured, the result is zeroed, and the binding re-panics in Go when the foreign
call returns.

A callback that wants to report a failure writes a status into its result and the
C++ side checks it. An exception raised *by* a callback terminates the process:
the same rule as everywhere else, applied to the reverse direction.

## Related

- Every error type and its fields: [Runtime]({{< ref "reference/runtime.md" >}}).
- What the generated signature looks like per declaration: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Why the C++ side is built at all: [Architecture]({{< ref "explanation/architecture.md" >}}).
