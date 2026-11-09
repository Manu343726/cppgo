---
title: "Limits"
description: "What cppgo cannot do, and what to do instead."
weight: 30
---

There are three kinds of limit, and they behave differently:

- **hard limits** — properties of the platform or the ABI, the *application
  binary interface*: how a compiled call is laid out, which registers and which
  memory carry its arguments and its result. No amount of generator work removes
  them.
- **not supported yet** — generation refuses the construct with a message naming
  the declaration and the fix. The construct never silently degrades.
- **designed** — stated in the design, not in this build. Marked as such wherever
  it appears in the documentation.

## Hard limits

Four things in this table need saying first. A **binding** is one C++ declaration
turned into a callable Go function. An exception *unwinds*: it walks the frames
above the `throw`, looking for a `catch` (a handler), and needs unwind information
for every frame it crosses. A *foreign call* is a call from Go into machine code
the Go toolchain never compiled. The *register file* is the fixed set of CPU
registers a calling convention passes arguments in — nothing on the stack counts,
and the callback path reads only those registers.

| Limit | Why | What you can do |
|---|---|---|
| An exception with no C++ frame terminates the process | the [direct path]({{< ref "explanation/carriers.md" >}}#direct) has no handler above it, and unwinding through Go frames is not supported, because a Go frame carries no unwind information | give the library a `noexcept` entry point — one that promises not to throw — that reports failure as a status |
| A callback cannot be called from a direct binding | the runtime only accepts a callback on a thread already inside a foreign call | none: bindings that take a Go function use the [reentrant carrier]({{< ref "explanation/carriers.md" >}}#reentrant) automatically |
| A callback carries at most 6 integer and 8 floating-point arguments, and one result | the callback entry receives the register file and nothing else | take a pointer to a struct |
| A `std::` layout appears only in C++ | constructing a `std::string` or `std::function` (C++'s wrapper around any callable) from a Go-side layout — the type's in-memory representation, the bytes and their order — works only for instantiations the library happens to export | let cppgo construct it (it does, for a parameter or a result) |
| A function-like macro that *is* the API cannot be bound | a macro is a text substitution performed before compilation, and its expansion is not a declaration | ask the library for a function |
| A template type nobody asked for does not exist | a template only exists where it is instantiated | run `cppgo build -scan` with the program that uses it |
| `long double` and `__int128` have no Go value type | they are the two C++ types wider than 64 bits, and there is no Go type of that width | widen or narrow at the C++ boundary |
| The C++ side must share your library's ABI and standard library | the two call each other across that boundary | build with the same compiler family and standard library; nothing detects a mismatch for you |

## Not supported yet

Each message names the declaration and what you can supply today. The messages
and the fixes use a handful of terms that need defining first. A **record** is a
struct or class used as a bundle of fields, and it is **mirrorable** when Go can
hold its bytes directly — every field public, the natural Go layout, and no
pointer, reference or other field Go cannot represent. A **POD** — *plain old
data* — is the strongest form of that: a record with no constructor, no virtual
function and no private members, whose bytes are its whole representation. An
**opaque record** is the other extreme: a C++ struct whose layout Go cannot see,
so Go holds only a pointer to storage C++ owns. A **bitfield** packs several
fields into the bits of one integer, a **packed struct** has had the padding
between its fields removed, and a **union** lets several members share the same
storage. The messages also name the convention's two classes of argument
register: **INTEGER**, the six general-purpose ones, and **SSE**, the eight
floating-point ones beside them.

| Construct | Message | Workaround |
|---|---|---|
| enum definitions | `enums are not emitted by this build yet; pass the value as its underlying integer at the C++ boundary` | pass the integer |
| a type nested inside a record | `a type nested inside a record is not emitted by this build yet; move the nested type to namespace scope, or bind the outer type through a C++ wrapper` | move it out, or wrap |
| base classes, inherited members, base views | `inherited members and base views are not emitted by this build yet; bind the base class directly, or expose the derived members through a C++ wrapper` | bind the base separately |
| a static data member | `the static data member X is not emitted by this build yet; expose it through a static member function or a free function` | a function |
| a member function template | `the member function template X is not emitted by this build yet; expose the concrete instantiation as a non-template member` | a non-template member |
| operators and conversion operators | `operators and conversion operators are not emitted by this build yet; give it a named member function or a named free function` | a named function |
| a variadic function (one whose parameter list ends in `...`) | `a variadic function has no fixed argument list to lay out; replace the ellipsis with a fixed parameter list or a span` | a fixed list |
| `std::unique_ptr`, `std::shared_ptr` | `"…" is a smart-pointer parameter` / `"…" is a smart-pointer result; return the object by value or through a reference at the C++ boundary` | a reference or a value |
| more than 6 integer / 8 floating-point arguments | `the call needs N INTEGER and M SSE argument registers; passing an argument block is not implemented by this build yet` | take a pointer to a struct |
| a non-POD passed by value | `"…" is passed by value but is not a POD; take it by const reference, or make it a POD that Go can mirror` | const reference |
| a pointer to an opaque record as a result | `"…" is a pointer to an opaque record; return it through a reference at the C++ boundary` | a reference |
| `void *` | `"…" is a void pointer; expose a typed pointer at the C++ boundary` | a typed pointer |
| `std::nullptr_t`, incomplete types (declared in the header but never defined) | `not part of the generated API` | include the definition, or drop the parameter |
| bitfields, packed structs, unions, private or mixed-access fields, a field of a non-mirrorable type | `the public field X of an opaque record is not emitted by this build yet; make the field private and expose an accessor in C++, or make the record a POD` | make it private with an accessor, or make the record a POD |
| a non-`const` reference to a `std::` value | `"…" is a non-const standard-library reference` | take it by value or by `const` reference |

### `char *` parameters

`char *` and `const char *` do not bind, with or without a paired length:
generation fails with a C++ conversion error. It is a defect, not a design
decision — the *type map*, the generator's table saying which C++ type becomes
which Go type, maps a `(char *p, size_t n)` pair to a Go `string`. Until it is
fixed, use `std::string` or `std::string_view` — a non-owning pointer-and-length
view of a string — at the boundary.

## Designed, not shipped

The first row is the *error projection*: turning each of the ways a C++ library
reports failure into a Go `error`.

| Item | What is missing | What you can rely on today |
|---|---|---|
| the full error projection | `std::error_code` (a library's own error value), `std::expected` (a value, or the error instead), `errno` (C's per-thread error number), `std::variant` (one of several types), a configured `bool` + out-parameter, custom result types | exceptions and `-status` become Go errors; a function returning `std::error_code` is refused at generation time |
| generics breadth, operators, inheritance | class templates bind for scalar type arguments and non-type dimensions (a template parameter that is a value rather than a type); operators, member templates, base classes and enums are refused | every refusal names the declaration and the fix |
| the image, array and matrix work of the three target libraries | `cv::Mat`, `af::array` and Eigen's matrix types: the generator refuses a record that declares something it cannot carry, and an Eigen matrix specialization is not a record it can read | each library ships a slice that binds and tests the declarations the generator carries today: [OpenCV](https://github.com/Manu343726/cppgo/tree/main/examples/opencv), [ArrayFire](https://github.com/Manu343726/cppgo/tree/main/examples/arrayfire), [Eigen](https://github.com/Manu343726/cppgo/tree/main/examples/eigen) |
| embedded generated packages | `cppgo.OpenFS` works; `Open`, the load function `cppgo gen` writes, does not call it yet | deploy the two C++ files, or use the runtime directly |

## What is not a limit

Two things look like limits and are not:

- **A missing declaration in the report.** The *report* is the list of bindings
  `cppgo gen` prints. If a declaration is absent from it, the declaration was not
  selected ([`-bind` / `-exclude`]({{< ref "reference/cli.md" >}}#cppgo-gen)) or
  the *parse* — the generator's reading of your headers — did not see it. A parse
  without the project's real flags says so with a warning; a wrong include path
  binds a different program.
- **A call that cannot use the direct path.** It is not a degradation of the API:
  the shim — the C++ file cppgo generates — carries the call, and the Go-side
  cost of it is the same
  ([Performance]({{< ref "explanation/benchmarks.md" >}})).

## Related

- What each construct costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
- Why these limits exist: [Architecture]({{< ref "explanation/architecture.md" >}}).
- Which construct gets which carrier: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
