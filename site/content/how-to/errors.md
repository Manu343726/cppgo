---
title: "Report failures from C++ to Go"
description: "Exceptions and status codes today; the full projection as designed."
weight: 50
---

A C++ library reports failure in more than one way: it throws, it returns a
`std::error_code`, a status integer, an `std::optional`, a project-specific
result type. A Go caller should not have to know which one it is looking at.
This guide covers what the generated package does today, and marks the part
that is designed and not shipped.

The rule that shapes every generated signature:

> **A call that can fail returns `(T, error)`, the value first and the error
> last. A call that cannot fail returns no error at all.**

There is no error value that is always nil, and a failure is never turned into a
zero result silently.

## Exceptions, today

A declaration that is not `noexcept` is carried by a thunk, and the thunk wraps
the call in `try`/`catch` — the call *and* the marshalling, because constructing
a `std::string` or a `std::vector` can throw `std::bad_alloc` too. This is the
generated code for `mathx::Vector::norm`
([`examples/mathx/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp)):

```cpp
extern "C" int32_t cppgo_thunk_49993cda6560(const void * self, double * cppgo_result) {
	cppgo_error_message.clear();
	try {
		*cppgo_result = static_cast<const ::mathx::Vector *>(self)->norm();
		return 0;
	} catch (const std::exception &error) {
		cppgo_error_message = error.what();
		return 1;
	} catch (...) {
		cppgo_error_message = "unknown C++ exception";
		return 1;
	}
}
```

The mechanism in four sentences: each thunk clears a thread-local message on
entry; a caught exception copies `what()` into it; the shim exports it through
`cppgo_last_error`; the generated binding reads it after a non-zero status and
returns it.

What the caller sees is a `*cppgo.CallError` naming the C++ declaration, wrapping
a `*cppgo.NativeError` that carries the text:

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
value in an out-parameter. Name that declaration with `-status`:

```console
$ cppgo gen -header include/status.hpp -package status \
            -library build/libstatus.so -status 'status::parse' -out ./status
```

The generated Go signature drops the integer and returns only the error:

```go
// Parse calls status::parse.
//
// C++: status::parse(const std::string &), _ZN6status5parseERKNSt7__cxx1112basic_stringIcSt11char_traitsIcESaIcEEE (thunk cppgo_thunk_2e40c86383f2, standard-library type: the thunk constructs, copies and destroys it)
func Parse(text string) error
```

The thunk returns the callee's own code to Go, and Go treats any non-zero value
as the failure. The conventions are pinned by `TestStatusConventionShape` and
`TestStatusEndToEnd` in `internal/gen`.

`-status` refuses two entries, because guessing would be worse than failing: a
name that matches no declaration (`it names nothing this header declares`), and
a declaration whose return is a value rather than an integer (`and a status is a
32-bit integer`). A function that returns `double` and takes an error
out-parameter is the `out_param` convention below, not this one.

## What a full projection looks like (designed)

The contract maps eight conventions to those same two Go shapes. Only the
exception and status rows are wired into `cppgo gen` today. The shared projection
(code, category, message) exists and is tested in `internal/errormap`, but the
generator does not read it yet: there is no `-errors` flag, and a
`cppgo.yaml` is not a generation input.

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

Designed, not shipped:
[issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19). Until it
lands, a library that returns `std::error_code` is refused at generation time,
and the message says what to do: cross the failure as a scalar or a wrapper with
a supported signature, or use `-status` when the convention is an integer.

## The rule you cannot configure away

**No exception can be supported without a C++ frame above the call.** A direct
binding is `noexcept` by construction, and a C++ exception thrown with no C++
frame above it terminates the process. That is measured three times in
`internal/callback`, from a direct call, after a callback returned, and through a
Go callback frame. It is why a throwing function is never assigned the direct
carrier, even when its signature would fit the registers.

The practical consequence: if your library's API throws and its headers cannot
be included by the shim (a private implementation, a macro-only API), that call
cannot be bound safely. The fix belongs in the library — a `noexcept` entry point
that returns a status — not in the Go side.

## Callbacks and panics

A Go function passed to C++ may panic. The panic does not unwind into C++: the
dispatcher recovers it, zeroes the result, and the binding re-panics in Go when
the foreign call returns. This is measured by `TestCallbackPanicIsCaptured` and
`TestCallbackPanicReachesTheCaller` in `internal/callback`.

An exception *raised by* a callback falls outside the ABI, so a callback that
wants to report a failure writes a status into its argument frame and the C++
side checks it. That is the same rule as everywhere else: the C++ side catches,
the Go side reads a status.

## Related

- Every error type and its fields: [Runtime]({{< ref "reference/runtime.md" >}}).
- What the generated signature looks like per declaration: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Why the shim exists at all: [Architecture]({{< ref "explanation/architecture.md" >}}).
