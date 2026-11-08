---
title: "Carriers and the construct table"
description: "How each C++ construct reaches C++, and which ones are supported."
weight: 25
---

This page is the mechanism behind a call: which of the three carriers a
declaration gets, and the table of constructs with the ones supported today. It
is the companion to [Architecture]({{< ref "explanation/architecture.md" >}});
the API a caller sees is in [Generated API]({{< ref "reference/generated-api.md" >}}),
and the cost of each carrier is in [Performance]({{< ref "explanation/benchmarks.md" >}}).

Every binding is assigned exactly one **carrier** before anything is emitted. The
carrier is how one call crosses from Go to C++, and it is invisible in the
generated API — it shows up in the report and in the comment on each binding.

## Direct

The generated assembly calls the C++ symbol in your library. All of these must
hold:

- the declaration has an out-of-line body, and its symbol is exported by the
  library (a header-only declaration whose symbol happens to be exported is
  direct too);
- it is **not virtual** — the call goes to a fixed address, so dispatch is a
  concern of the C++ side;
- it is **`noexcept`** — the direct path has no handler above it, and an escaping
  exception terminates the process;
- the signature is register-representable: scalars, enums, pointers and
  references, and a POD of at most 16 bytes with no MEMORY-class part. No
  `std::` type.

## Thunk

A generated `extern "C"` function that cppgo compiles into the C++ side it
builds, and that calls the declaration as C++. Everything the direct path cannot
express goes here: inline bodies, virtual members, `std::` types, by-value
records, constructors and destructors, and every call that may throw.

The thunk is where exceptions are caught: each one clears a thread-local message,
runs the call inside `try`/`catch`, and returns a status. That is why a
non-`noexcept` declaration is never direct, whatever its signature.

## Reentrant

A thunk called through `runtime.cgocall` instead of the assembly trampoline. It
is the only state in which the Go runtime accepts a callback: a call on the
direct path leaves the thread outside a foreign call, and a callback reached that
way kills the process. A binding whose signature passes a Go function to C++
therefore gets this carrier.

Template calls go through this carrier as well: the generated generic code calls
`cppgo.CallReentrant`, so a template binding costs what the reentrant carrier
costs. That is one implementation detail the generated package exposes in its
source, not in its API.

## The report

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
```

In `examples/stats`, four `noexcept` functions take the direct carrier and
`histogram` takes a thunk: that is the whole difference the header makes.

## The construct table

"Supported" means a generated package does it today. "Refused" means generation
stops with a message naming the declaration and the fix. "Designed" means the
mechanism is stated in the design and is not in this build.

| Construct | Carrier | Status |
|---|---|---|
| inline / header-only body | thunk | supported |
| virtual member function | thunk (real vtable dispatch) | supported |
| virtual destructor | thunk | supported |
| multiple / virtual inheritance | thunk (`static_cast`/`dynamic_cast` for `this`) | measured, **refused by the generator**: base classes are not emitted yet |
| `std::string` | thunk (constructed and copied in the C++ side) | supported |
| `std::string_view`, `std::span` | thunk (borrowed, valid for the call) | supported |
| `std::vector<T>` | thunk | supported |
| `std::array<T,N>` | thunk, by value | supported |
| `std::optional<T>` | thunk; Go value is `*T` | supported |
| `std::unique_ptr`, `std::shared_ptr` | — | **refused** |
| POD ≤ 16 bytes by value | thunk | supported |
| POD > 16 bytes / MEMORY class by value | thunk, through an sret pointer | generated and compiled, not executed |
| non-POD by value | thunk | measured, **refused by the generator** |
| more than 6 INTEGER / 8 SSE arguments | — | **refused**: the argument block is not implemented |
| exception-throwing call | thunk (`try`/`catch`) | supported |
| callback parameter, `std::function` | reentrant | emitted, not exercised by an example |
| class / function template | thunk, called reentrantly | supported |
| static member | thunk, no `this` | supported |
| enum | — | **refused** |
| operators, conversion operators | — | **refused** |
| function-like macro that is the API | — | not bindable: a macro's expansion is not a declaration |

## Related

- The mechanism in prose: [Architecture]({{< ref "explanation/architecture.md" >}}).
- What the generated signatures look like: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Everything that is refused, with the message: [Limits]({{< ref "explanation/limits.md" >}}).
- What each carrier costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
