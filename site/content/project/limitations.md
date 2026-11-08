+++
title = "Limitations and roadmap"
description = "The v4 scope stated plainly: what the direct and thunk carriers handle, the constructs the emitter does not bind yet with their exact messages, the parts designed but not wired, and the costs of the runtime."
weight = 30
+++

**Audience:** you are deciding whether cppgo covers your library, or you hit a
refusal and want to know who owns it.

cppgo is early, and the honest version of its scope is more useful than a
feature list. Everything here is a current limitation.

## Platform

| | status |
|---|---|
| **Linux/amd64** | supported; the only target that can execute a binding |
| arm64 | not supported. The package builds, and a call panics with a clear message |
| Windows | not supported (no `dlopen`/SysV in this form) |
| macOS | not supported at runtime. The generator builds and libclang discovery includes Homebrew paths, but the trampolines are amd64 SysV |
| Go | 1.25 or newer (the module pins `go 1.25.0`) |

## What the v4 carriers handle

| construct | carrier | evidence |
|---|---|---|
| free function, `static` member, `const` member, constructor, destructor | direct when `noexcept`, out of line and register-representable | the mathx and stats examples |
| inline or header-only body | thunk. The shim includes the header and compiles the body | `X()` in `examples/mathx` |
| virtual method and virtual destructor | thunk. The call dispatches through the vtable | `internal/shim` construct matrix |
| `std::string`, `std::vector` | thunk. The shim constructs, copies and destroys the value | `Describe`, `Window` in the project fixture |
| record by value, including a result above 16 bytes | thunk. The shim copies it in and writes the result through the `sret` pointer | `Midpoint(Point, Point) (Point, error)` |
| exception-throwing function | thunk. The shim catches, and the message becomes a Go error | `Ratio(6, 0)`, `stats::histogram` |
| function pointer or `std::function` parameter | reentrant. The call enters through `runtime.cgocall` so the callback is legal | `Apply`, `Fold`, `Integrate` |
| concrete class template specialisation | shim, named with `-instantiate` | `CellInt`, `CellFloat64` |
| a 32-bit integer return read as a status | `-status`, which removes the integer from the Go result | `manual/errors` |

The carrier is chosen from the declaration. There is no mode to select and no
directive to write.

## What the emitter does not bind yet

Most of these are generation errors that write no files. The message names the
file, the line, the declaration, what is wrong and what to supply. Two cases
behave differently, and they are stated after the table.

| input | message | owner |
|---|---|---|
| an enum, anywhere in the parsed headers | *enums are not emitted by this build yet; pass the value as its underlying integer at the C++ boundary* | the v4 emitter, epic #1 |
| inheritance, including `As<Base>` | *inherited members and base views are not emitted by this build yet; bind the base class directly, or expose the derived members through a C++ wrapper* | the v4 emitter, epic #1 |
| an operator or conversion operator | *operators and conversion operators are not emitted by this build yet; give it a named member function or a named free function* | the v4 emitter, epic #1 |
| a static data member | *the static data member X is not emitted by this build yet; expose it through a static member function or a free function* | the v4 emitter, epic #1 |
| a member function template | *the member function template X is not emitted by this build yet; expose the concrete instantiation as a non-template member* | the v4 emitter, epic #1 |
| a free function template | *a function template has no symbol until it is instantiated; expose a concrete instantiation as a non-template function* | the v4 emitter, epic #1 |
| a nested type | *a type nested inside a record is not emitted by this build yet; move the nested type to namespace scope, or bind the outer type through a C++ wrapper* | the v4 emitter, epic #1 |
| a variadic function | *a variadic function has no fixed argument list to lay out; replace the ellipsis with a fixed parameter list or a span* | the v4 emitter, epic #1 |
| a class template with no `-instantiate` entry | *a class template is not a type until it is instantiated; name a concrete instantiation with `-instantiate 'Cell<...>'`* | input the user supplies |
| `long double` or `__int128` in a signature | *cppgo: long double: long double and __int128 have no Go value type; widen or narrow it at the C++ boundary to double or int64, or wrap it in a representable type* | a wrapper with a representable type |

A **function-like macro that is the whole API** emits no error today.
libclang sees the macro's expansion and never its declaration. The package binds nothing.
`cppgo gen` exits 0, writes a package whose only exported function is `Open`,
and builds the shim. The fix is a function, which every large library provides.
The owner is the v4 emitter, which should report the unbound macro.

A **template instantiation that nobody asked for** is a load-time error in the designed two-phase flow. See the next section.

One consequence surprises people. The refusal is checked before selection. A header that declares any enum anywhere fails even when you did not bind it.
Splitting the header is the current workaround. The full refusal table, with
the parameter that triggers each one, is in
[validation]({{< ref "manual/validation.md" >}}).

## Designed, measured in the prototype, not emitted by the generator

- **the full error projection** — `std::error_code`, `std::expected`,
  `std::optional`, `errno`, status ints, `bool` + out-parameters and custom
  result types. The mechanism and its configuration are implemented and
  measured in `internal/errormap`. `cppgo gen` has yet to read it
  ([#19](https://git.manu343726.com/manu343726/cppgo/issues/19)). Today the
  generator emits exceptions and `-status`.
- **the generic Go API for templates (4b-1)** — concrete specialisations work
  through `-instantiate`. `Matrix[float64,4,4]` and the automatic collection of
  the instantiations a Go program uses are the next phase, under epic #1. In
  that flow a Go instantiation with no C++ counterpart is a load-time error
  naming the exact `template …;` line to add. Instantiation collection is still 4b-1 work, so that error waits for it.
- **the target libraries** — Eigen, OpenCV and ArrayFire, as generated packages
  tested against the real headers and the real shared object
  ([#15](https://git.manu343726.com/manu343726/cppgo/issues/15)).
- **`std::string_view`, `std::span`, `std::array`** — mapped by the emitter,
  with no end-to-end fixture yet.
- **`std::optional` as a parameter or result, and `std::unique_ptr`** — mapped,
  with no end-to-end fixture yet.
- **`std::shared_ptr`** — mapped, with no support yet.
- **embedding wired into `Open`** — `cppgo.OpenFS` ships in the runtime. A
  generated package's `Open` takes a path today
  ([#20](https://git.manu343726.com/manu343726/cppgo/issues/20)).

## The runtime trade-offs

- **The `P` stays with the goroutine during a direct or thunk call.** Short
  calls are fast. A blocking call delays a GC cycle by up to its duration, so
  move blocking work to a separate process ([Why cppgo]({{< ref
  "project/why.md" >}})).
- **The foreign frame lives on the goroutine stack**, inside a 32 KiB reserve.
  Deep recursion or a large C++ stack frame can exhaust it.
- **A callback-capable binding costs about ten times a direct one** (151 ns
  against 15.9 ns, measured 2026-11-08). A callback carries at most six integer
  and eight floating-point arguments and one register result.
- **The binary is dynamically linked.** The promise covers the build, with no
  cgo and no C toolchain.
- **The shim and the library must share a C++ ABI and standard library.**
  Mixing libstdc++ and libc++ is a generation error. The compiler family, the
  standard library and `_GLIBCXX_USE_CXX11_ABI` are part of the contract.
- **An exception cannot cross a Go frame.** A direct binding is `noexcept` by
  construction, and a throw with no C++ frame terminates the process. The shim
  is the only place a throw is caught.
- **No finalizers.** Every object needs an explicit `Close`.
- **`Library.Close` leaves in-flight calls running.** The caller owns the
  ordering.
- **One user library per Go package.** The package loads one shim linked
  against one library. Two libraries mean two packages.

## What works today

- **No cgo at build time.** `CGO_ENABLED=0` builds and tests the runtime.
- **No `import "C"` anywhere.**
- **No hand-written bindings.** The header and the flags are the input.
- **Symbols are resolved at run time.** The same binary can load a different
  build of the library.
- **The library stays untouched.** No generated file goes into your build.

## Roadmap

In the order the design records it, with no dates:

1. the remaining constructs in the emitter (enums, inheritance, operators,
   static data, nested types, member templates, variadic functions).
2. the error projection (#19) and `cppgo.yaml` as a generation input.
3. the generic Go API for templates (4b-1).
4. the target slices (Eigen, OpenCV, ArrayFire, #15).
5. embedding wired into `Open` (#20).
6. arm64.

None of these are promised by this documentation. They are the directions the
design contract names. If a page or a comment here reads as a feature, and the
code has not done it, the code is right and the page is wrong. Please treat it
that way.

Next is [FAQ]({{< ref "project/faq.md" >}}).
