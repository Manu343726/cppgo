---
title: "Limits"
description: "What cppgo cannot do, with the measurement behind each limit."
weight: 30
---

There are three kinds of limit, and they behave differently:

- **hard limits** — properties of the platform or the ABI. No amount of
  generator work removes them. Each one has a test that measures the failure.
- **not emitted yet** — the generator refuses the construct with a message
  naming the declaration and the fix. The work is defined; the construct does
  not silently degrade.
- **designed** — stated in `DESIGN.md`, not in this build. Marked as such
  wherever it appears in the documentation.

## Hard limits

| Limit | Why | Evidence |
|---|---|---|
| An exception with no C++ frame terminates the process | the direct path has no handler above it, and unwinding through Go frames is not supported | `TestExceptionFromCIsFatal`, `TestExceptionAfterACallbackIsFatal`, `TestExceptionThroughTheGoCallbackIsFatal` in `internal/callback` |
| A callback requires the reentrant carrier | the runtime only accepts a callback on a thread already inside a foreign call | `TestCallbackOnTheDirectTrampolineIsFatal` — `fatal error: morestack on g0` |
| A callback carries at most 6 INTEGER and 8 SSE arguments and one register result | the dispatcher receives the register file and nothing else | `internal/callback`'s trampoline tests; `TestGenerationErrors` (`callbackargs.hpp`, `callbackmanyfloats.hpp`) |
| `runtime.cgocall` needs the cgo runtime initialised | the reentrant entry calls it by linkname, and it throws `cgocall unavailable` without it. Importing the cppgo package pulls in `purego`, whose fakecgo does it under `CGO_ENABLED=0` | the runtime's own test |
| A `std::` layout ever appears only in C++ | constructing a `std::string` or `std::function` from a documented Go-side layout works only for instantiations the library happens to export | `TestStdFunctionFromGo` in `internal/callback` keeps the fallback |
| A function-like macro that *is* the API cannot be bound | libclang sees a macro's expansion, not its declaration | `DESIGN.md` § *Hard limits* |
| A template instantiation nobody asked for does not exist | a template only exists where it is instantiated; the two-phase flow is the answer | `TestTwoPhaseGenerics` (`int64` subtest) |
| `long double` and `__int128` have no Go value type | there is no Go type of that width | `TestGenerationErrors` (`longdouble.hpp`, `int128.hpp`) |
| The shim and the library must share a C++ ABI and standard library | the shim calls the library across that boundary; nothing at load time detects a mismatch | the report names the compiler that built the shim, so the two can be compared by hand. Recording the version and `_GLIBCXX_USE_CXX11_ABI` and refusing a mismatch is designed, not implemented |

## Not emitted yet

The single list is `rejectUnimplemented` in `internal/gen/reject.go`, plus the
type mapper's own refusals. Each message names the declaration and what you can
supply today.

| Construct | Message | Workaround |
|---|---|---|
| enum definitions | `enums are not emitted by this build yet; pass the value as its underlying integer at the C++ boundary` | pass the integer |
| a type nested inside a record | `a type nested inside a record is not emitted by this build yet; move the nested type to namespace scope, or bind the outer type through a C++ wrapper` | move it out, or wrap |
| base classes, inherited members, base views | `inherited members and base views are not emitted by this build yet; bind the base class directly, or expose the derived members through a C++ wrapper` | bind the base separately |
| a static data member | `the static data member X is not emitted by this build yet; expose it through a static member function or a free function` | a function |
| a member function template | `the member function template X is not emitted by this build yet; expose the concrete instantiation as a non-template member` | a non-template member |
| operators and conversion operators | `operators and conversion operators are not emitted by this build yet; give it a named member function or a named free function` | a named function |
| a variadic function | `a variadic function has no fixed argument list to lay out; replace the ellipsis with a fixed parameter list or a span` | a fixed list |
| `std::unique_ptr`, `std::shared_ptr` | `"…" is a smart-pointer parameter` / `"…" is a smart-pointer result; return the object by value or through a reference at the C++ boundary` | a reference or a value |
| more than 6 INTEGER / 8 SSE arguments | `the call needs N INTEGER and M SSE argument registers; passing an argument block is not implemented by this build yet` | take a pointer to a struct |
| a non-POD passed by value | `"…" is passed by value but is not a POD; take it by const reference, or make it a POD that Go can mirror` | const reference |
| a pointer to an opaque record as a result | `"…" is a pointer to an opaque record; return it through a reference at the C++ boundary` | a reference |
| `void *` | `"…" is a void pointer; expose a typed pointer at the C++ boundary` | a typed pointer |
| `std::nullptr_t`, incomplete types | `not part of the generated API` | include the definition, or drop the parameter |
| bitfields, packed structs, unions, private or mixed-access fields, a field of a non-mirrorable type | `the public field X of an opaque record is not emitted by this build yet; make the field private and expose an accessor in C++, or make the record a POD` | make it private with an accessor, or make the record a POD |
| a non-`const` reference to a `std::` value | `"…" is a non-const standard-library reference` | take it by value or by `const` reference |

The tests are `TestGenerationErrors` and `TestEnumDiagnostic` in `internal/gen`;
each row above is a fixture under `internal/gen/testdata/errors/`.

### A measured defect: `char *` parameters

`char *` and `const char *` do not bind, with or without a paired length. The
parameter is mapped to `*int8`, and the generated shim then fails to compile:

```
cppgo gen: cppgo: zz_cppgo_shim.cpp:34: ch::take: the generated shim does not
compile; invalid conversion from 'const int8_t*' {aka 'const signed char*'} to
'const char*' [-fpermissive]
```

Measured 2026-11-08 with the generator at this commit. It is a defect, not a
design decision: `DESIGN.md`'s type map says `(char *p, size_t n)` becomes a Go
`string`. Until it is fixed, use `std::string` or `std::string_view` at the
boundary.

## Designed, not shipped

| Item | What is missing | Where it is tracked |
|---|---|---|
| the full error projection | `std::error_code`, `std::expected`, `errno`, `std::variant`, a configured `bool` + out-parameter, custom result types. The projection is implemented and tested in `internal/errormap`; `cppgo gen` does not read it, and `cppgo.yaml` is not a generation input | [issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19) |
| generics breadth, operators, inheritance | class templates bind for scalar type arguments and non-type dimensions; operators, member templates, base classes and enums are refused | the wider generator work; see the table above |
| the target slices: OpenCV, ArrayFire, Eigen | a generated binding tested against each real library, with its own build tag and a skip when the library is absent. The two-phase flow exists for Eigen; no slice is implemented | epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1) |
| embedded generated packages | `cppgo.OpenFS` works and is measured; the `Open` that `cppgo gen` writes does not call it yet | [ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md) |

## What is not a limit

Two things look like limits and are not:

- **A missing declaration in the report.** If a declaration is absent, it was
  not selected (`-bind`/`-exclude`) or the parse did not see it. A parse without
  the project's real flags says so with a warning; a wrong include path binds a
  different program.
- **A non-`noexcept` function taking the thunk carrier.** That is not a
  degradation of the API: it is the only safe carrier, and the cost is one C++
  call frame on top of the same Go-side trampoline
  ([measured]({{< ref "reference/carriers.md" >}})).

## Related

- What the mechanisms cost: [Carrier table]({{< ref "reference/carriers.md" >}}).
- Why these limits exist: [Architecture]({{< ref "explanation/architecture.md" >}}).
- The decisions behind them: [Design decisions]({{< ref "explanation/decisions.md" >}}).
