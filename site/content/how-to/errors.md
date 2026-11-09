---
title: "Report failures from C++ to Go"
description: "Exceptions and status codes today; the full projection as designed."
weight: 50
---

A C++ library reports failure in more than one way: it throws, it returns a
`std::error_code`, a status integer, an `std::optional`, a project-specific
result type. A Go caller should not have to know which one it is looking at.

This guide is for the point where a bound call can fail: it says what reaches
Go, which conventions this build supports today, and which it refuses. The
scenario is the `metrics` library of
[the complete example](https://github.com/Manu343726/cppgo/tree/main/examples/full):
one function throws, one reports a status code, and the example program calls
both failure paths.

The rule that shapes every generated signature:

> **A call that can fail returns `(T, error)`, the value first and the error
> last. A call that cannot fail returns no error at all.**

There is no error value that is always nil, and a failure is never turned into a
zero result silently.

## Exceptions

A C++ function or method that can throw becomes a Go call that returns an error.
The declaration, from
[`metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp):

```cpp
double ratio(double numerator, double denominator);
```

`ratio` can throw, so its Go signature carries the error. From the generated
[`gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
// Ratio calls metrics::ratio.
//
// C++: metrics::ratio(double, double), _ZN7metrics5ratioEdd (thunk cppgo_thunk_d4489c841d72, not noexcept: an exception would unwind into Go), verified against libmetrics.so
func Ratio(numerator float64, denominator float64) (float64, error)
```

When the call fails, the error is a `*cppgo.CallError` naming the C++
declaration, wrapping a `*cppgo.NativeError` that carries the library's message.
The example program prints both layers in one line:

```console
$ make run
ratio(1, 0) failed as expected: cppgo: metrics::ratio: division by zero
```

Both types have `Unwrap`, so `errors.As` finds either one:

```go
var call *cppgo.CallError
if errors.As(err, &call) {
	fmt.Println("declaration:", call.Symbol)   // metrics::ratio
}

var native *cppgo.NativeError
if errors.As(err, &native) {
	fmt.Println("library said:", native.Message) // division by zero
}
```

`errors.Is` works for the sentinel `cppgo.ErrClosed`, which is what a method on a
nil or closed receiver returns. A `CallError` has no comparable identity, so
`errors.Is` on one answers false; use `errors.As`.

## A status code

Some C libraries report a class of failures as a 32-bit return code and put the
value in an out-parameter. The example has one: `validate_name` returns 0 for a
valid name and a non-zero status otherwise. From
[`metrics.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/full/include/metrics/metrics.hpp):

```cpp
// Status convention: 0 means success, any other value is a failure. The
// generator is told about it with -status, because a 32-bit integer alone does
// not say that it is a status.
std::int32_t validate_name(const std::string &name);
```

An integer return alone does not say that it is a status, so the generation
command names it. The example's `make gen` carries the flag:

```console
$ make gen
build/cppgo gen -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name
```

The generated Go signature drops the integer and returns only the error, from
[`gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
// ValidateName calls metrics::validate_name.
//
// C++: metrics::validate_name(const std::string &), _ZN7metrics13validate_nameERKNSt7__cxx1112basic_stringIcSt11char_traitsIcESaIcEEE (thunk cppgo_thunk_492cf416925a, standard-library type: the thunk constructs, copies and destroys it), verified against libmetrics.so
func ValidateName(name string) error
```

Any non-zero return is the failure. The program calls a valid name and then the
empty one; the second prints:

```console
$ make run
validate_name("") failed as expected: cppgo: metrics::validate_name: the call failed without a message
```

`-status` refuses two entries, because guessing would be worse than failing: a
name that matches no declaration (`it names nothing this header declares`), and
a declaration whose return is a value rather than an integer (`and a status is a
32-bit integer`). A function that returns `double` and takes an error
out-parameter is the `bool` + out-parameters row below, not this one.

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

The last column is the design; the "today" column is what this build does. A
library that returns `std::error_code` is refused at generation time, and the
message says what to do: cross the failure as a scalar or a wrapper with a
supported signature, or use `-status` when the convention is an integer.

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
