---
title: "Carrier table"
description: "How each C++ construct crosses into Go, and the test that measures it."
weight: 40
---

Every binding is assigned exactly one **carrier** before anything is emitted. The
carrier is how one call crosses from Go to C++, and it is invisible in the
generated API — it shows up in the report, in the evidence comment, and in what
the runtime does underneath.

There are three carriers.

## Direct

The generated NOSPLIT trampoline calls the mangled C++ symbol in your library.
All of these must hold:

- the declaration has an out-of-line body, and its mangled name is in the
  library's `.dynsym` (a header-only declaration whose symbol happens to be
  exported is direct too);
- it is **not virtual** — the call goes to a fixed address, so dispatch is a
  shim concern;
- it is **`noexcept`** — the direct path has no handler above it, and an escaping
  exception terminates the process;
- the signature is register-representable: scalars, enums, pointers and
  references, and a POD of at most 16 bytes with no MEMORY-class part. No
  `std::` type.

## Thunk

A generated `extern "C"` function that cppgo compiles into the shim, and that
calls the declaration as C++. Everything the direct path cannot express goes
here: inline bodies, virtual members, `std::` types, by-value records,
constructors and destructors, and every call that may throw.

The thunk is where exceptions are caught: each one clears a thread-local
message, runs the call inside `try`/`catch`, and returns a status. That is why a
non-`noexcept` declaration is never direct, whatever its signature.

## Reentrant

A thunk called through `runtime.cgocall` instead of the NOSPLIT trampoline. It is
the only state in which the Go runtime accepts a callback: `asm.Call` leaves the
M outside a foreign call, and a callback reached that way kills the process
(`fatal error: morestack on g0`, measured). A binding whose signature passes a Go
function to C++ therefore gets this carrier.

Template calls go through this carrier as well, because a template member may
take a callback and the two-phase shim cannot know at generation time.

## What each costs

Measured on the machine that built this tree, `go test -bench .` with
`-benchtime 200000x`; the numbers are quoted in `DESIGN.md` (§ *The
trampoline*).

Carriers side by side, 2026-11-08:

| Path | ns/op |
|---|---|
| **cppgo trampoline, direct** | **15.9** |
| cppgo trampoline, reentrant | 151 |
| callback round trip | 481 |

Reference points, 2026-11-03, on the same machine pinned to 964 MHz with boost
disabled — the absolute nanoseconds there are inflated by roughly 4.5×, so the
ratio is the honest part:

| Path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| cppgo trampoline, direct | 13.9 | 13 |

The reentrant carrier costs one 16-byte allocation per call, because the
argument block that carries the target cannot live on a goroutine stack a
callback may grow. A thunk adds one C++ call frame inside the library; the Go
side pays the same trampoline either way.

## The report

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
```

## The construct table

"Shipped" means a generated package in this tree does it and a test asserts the
result. "Refused" means the generator stops with a message naming the
declaration and the fix. "Designed" means the mechanism is stated in
`DESIGN.md` and is not in this build.

| Construct | Carrier | Status | Evidence |
|---|---|---|---|
| inline / header-only body | thunk | shipped | `mathx::Vector::x()` in `examples/mathx`; `TestInlineHeaderOnlyIsReachableOnlyThroughTheShim` |
| virtual member function | thunk (real vtable dispatch) | shipped, no generated example | `TestVirtualDispatchAndVirtualDestructor` in `internal/shim` |
| virtual destructor | thunk | shipped, no generated example | same test |
| multiple / virtual inheritance | thunk (`static_cast`/`dynamic_cast` for `this`) | mechanism measured, **refused by the generator**: base classes are not emitted yet | `TestMultipleInheritanceAdjustment` in `internal/shim`; `reject.go` |
| `std::string` | thunk (constructed and copied in the shim) | shipped | `TestStdFixtureEndToEnd` in `internal/gen` |
| `std::string_view`, `std::span` | thunk (borrowed, valid for the call) | shipped | `TestStdFixtureEndToEnd` |
| `std::vector<T>` | thunk | shipped | `TestStdFixtureEndToEnd`; `TestStdStringAndVector` |
| `std::array<T,N>` | thunk, by value | shipped | `TestStdFixtureEndToEnd` |
| `std::optional<T>` | thunk; Go value is `*T` | shipped | `TestStdFixtureEndToEnd` |
| `std::unique_ptr`, `std::shared_ptr` | — | **refused**: smart-pointer parameter/result are generation errors | `TestGenerationErrors` (`smartparam.hpp`, `smartresult.hpp`) |
| POD ≤ 16 bytes by value | thunk | shipped | `mathx::midpoint`; `TestByValueRecordShape` |
| POD > 16 bytes / MEMORY class by value | thunk, through an sret pointer | generated and compiled, not executed | `TestByValueRecordShape` (`bigresult.hpp`), `TestByValueRecordShimCompiles` |
| non-POD by value | thunk | mechanism measured, **refused by the generator** | `TestNonPODByValue` in `internal/shim`; `errors_test` (`opaquevalue.hpp`) |
| more than 6 INTEGER / 8 SSE arguments | — | **refused**: the argument block is not implemented | `manyints.hpp`, `manyfloats.hpp`; `model.go` |
| exception-throwing call | thunk (`try`/`catch`) | shipped | `mathx::Vector::at`, `validate`; `TestExceptionsBecomeErrors` |
| callback parameter, `std::function` | reentrant | emitted; mechanism measured, no generated example | `internal/callback`; `TestCallbackThroughTheShim` in `internal/shim`; `TestReentrantResult` |
| class / function template | thunk, called reentrantly | shipped | `TestTwoPhaseGenerics` in `internal/gen` |
| static member | thunk, no `this` | shipped | `mathx::Vector::dims()` → `VectorDims` |
| enum | — | **refused**: `enums are not emitted by this build yet` | `TestEnumDiagnostic` |
| operators, conversion operators | — | **refused** | `TestGenerationErrors` (`freeoperator.hpp`, `operand.hpp`) |
| function-like macro that is the API | — | invisible to libclang: libclang sees expansions, not declarations | `DESIGN.md`, *Hard limits* |

## Related

- The mechanism in prose: [Architecture]({{< ref "explanation/architecture.md" >}}).
- What the generated signatures look like: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Everything that is refused, with the message: [Limits]({{< ref "explanation/limits.md" >}}).
