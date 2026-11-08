+++
title = "Error handling"
description = "What v4 emits today — exceptions caught by the shim and -status — what a direct call can and cannot report, and the std::error_code / std::expected mapping that is designed and tracked in #19."
weight = 60
+++

**Audience:** you have a C++ function that can fail and want to know what the Go
caller sees.

## The rule

**Every call that can fail returns `(T, error)` — value first, error last — and
every call that cannot fail returns no error at all.** The generated signature
answers the question before you write any Go: a `noexcept` function that returns
`int` is `func(...) int32`, not `(int32, error)`, and the package contains no
error that is always nil.

The mechanism lives in the C++ shim, which can catch an exception and read a
status; the projection lives in the Go wrapper.

## Exceptions

A declaration that is not `noexcept` goes through a thunk. The thunk wraps the
call — and its argument marshalling — in `try`/`catch`, stores the message in a
thread-local string, and returns a status:

```cpp
extern "C" int32_t cppgo_thunk_d691e1c0b71e(int32_t a, int32_t b, int32_t * cppgo_result) {
	cppgo_error_message.clear();
	try {
		*cppgo_result = ::analytic::ratio(a, b);
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

On the Go side a non-zero status becomes a `*cppgo.CallError` naming the
declaration and wrapping the message:

```go
q, err := analytic.Ratio(6, 0)
// q == 0
// err: cppgo: analytic::ratio: division by zero
// errors.As(err, &callErr)  → *cppgo.CallError{Symbol: "analytic::ratio", ...}
// errors.As(err, &native)   → *cppgo.NativeError{Message: "division by zero"}
```

The message is copied out of C++ immediately, because `cppgo_last_error()` is
thread-local and valid only until the next call on the same thread. A library
that cannot fail does not export it at all; `cppgo.LastError` returns nil then.

The same path covers **construction and destruction**: a constructor that throws
returns `(nil, error)` and releases the object's storage; `Close` returns the
error the destructor stored. `bad_alloc` while marshalling a `std::string` or a
`std::vector` is caught by the same `try`.

<div class="callout callout-warn">
<span class="callout-title">An exception must not cross a Go frame</span>
<p>
The shim is the C++ frame that makes catching possible. A direct binding is
<code>noexcept</code> by construction, so there is nothing to catch: a throw out
of a function declared <code>noexcept</code> terminates the process. That is
measured, not folklore, in <code>internal/callback</code>'s child-process
tests. A library that throws and whose header cannot be included by the shim
cannot be bound safely; the fix is a <code>noexcept</code> entry point in the
library.
</p>
</div>

## `-status`: a 32-bit return that is a code

Some C++ APIs follow the C convention: the function returns an `int32_t`, `0` is
success, anything else is an error. The declaration alone cannot say that — a
`bool` or an `int` says nothing — so you name it:

```console
$ cppgo gen -header include/stats/stats.hpp -package stats \
      -library build/libstats.so -out ./stats -status stats::ensure
```

```go
func Ensure() error
```

A non-zero value is wrapped in `*cppgo.CallError`; the integer is not returned as
a value, because it never was one. The return type has to be a 32-bit integer:
`-status` on a `void` function or a `double` is a generation error naming the
type it found.

This is the whole error story v4 emits today, and it is measured: the exception
row by `TestExceptionsBecomeErrors` and the generated package's
`Ratio(6, 0)` assertion, the status row by the `-status` fixture.

## What is designed, and not emitted yet

[Issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19) specifies the
rest of the mapping. The mechanism is implemented and measured in
`internal/errormap`; the generator does not read it yet.

| C++ convention | how it is recognised | Go |
|---|---|---|
| `std::error_code` as a return | the return type is `std::error_code` | `(T, error)`; `*errormap.ErrorCode` with code, category and message; `errors.Is` compares code **and** category |
| `std::expected<T,E>`, `tl::expected` | the return type has `has_value()`/`value()`/`error()`, or is spelled `std::expected` | `(T, error)`; `E = std::error_code` → `ErrorCode`, `E = std::string` → `Message` |
| a custom result type (`outcome<T>`, `absl::StatusOr<T>`) | the shape `has_value`/`value`/`error` | `(T, error)`, with the error member projected |
| `std::optional<T>` | the return type is `std::optional<T>` | **not an error by default**: `(T, bool)`; a config entry with `absent:` makes it `(T, error)` |
| status int, `errno` | a config entry, never inferred | `(T, error)` with a named category |
| `bool` + out parameters | never inferred: a `bool` says nothing | a config entry names the value and error channels |
| `std::variant<T,E>` | never guessed | a config entry names the error alternative |

Two rules make the future API predictable:

- **no error is synthesized for a call that cannot fail** (already true);
- **no failure is dropped** — a declaration that can fail and whose convention
  is neither inferred nor configured will be a generation error naming the
  declaration, not a call whose failure becomes a zero value.

The configuration will be a `cppgo.yaml` beside the generation command, and it
will be a generation input, so `cppgo check` will fail when it changes without a
regeneration. None of that is true of today's `cppgo gen`: there is no `-errors`
flag and no `cppgo.yaml` reader outside `internal/errormap`'s tests.

## Library and symbol errors

These are Go errors from the runtime package, not statuses from C++:

| error | when | `errors.Is` / `errors.As` |
|---|---|---|
| `*cppgo.OpenError` | `dlopen` failed, or the path was empty | `errors.As`; `Unwrap` exposes the loader error |
| `*cppgo.SymbolError` | `dlsym` failed, or the library is closed | `errors.As`; `errors.Is(err, cppgo.ErrClosed)` |
| `cppgo.ErrClosed` | any symbol after `Close`, and a second `Close` | `errors.Is` |
| `*cppgo.CallError` | a thunk reported a non-zero status | `errors.As`; `Unwrap` exposes the native error |
| `*cppgo.NativeError` | the message from `cppgo_last_error` | `errors.As`; no `Unwrap` |
| `*cppgo.ExtractError` | an embedded object could not be extracted by `OpenFS` | `errors.As` |
| `cppgo.ErrInsecureCache` | the cache path is not a private directory | `errors.Is` |

A generated package loads its two objects on the **first call**. A binding with
an `error` result returns the load failure; a binding without one panics, which
is the intended severity for a program built against a library that is not
there:

```go
if err := cppgoOpen(""); err != nil {
	return 0, err        // func (v *Vector) Norm() (float64, error)
}
```

```go
if err := cppgoOpen(""); err != nil {
	panic(err)           // func (v *Vector) Scale(k float64)
}
```

A **panic inside a callback** never unwinds into C++: the dispatcher recovers
it, the binding checks after the foreign call returns, and re-panics on the Go
side. Measured by the project fixture's `Integrate` panic subtest.

## What is not an error

- A **zero value of a class type** is a nil object, not a failure. A method on
  it returns `cppgo.ErrClosed`.
- A **negative** status is not special: any non-zero value is a failure.
- **Parameter validation** is not done at call time. A nil slice arrives as
  `(nil, 0)`, a null `*T` as a null pointer, and the C++ side has to reject
  them. The generated code does not synthesize checks beyond the slice-length
  check described in [type mapping]({{< ref "manual/type-mapping.md" >}}).

Next: [object lifetimes]({{< ref "manual/lifetimes.md" >}}).
