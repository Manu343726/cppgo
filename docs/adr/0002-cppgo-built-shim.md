# ADR-0002: cppgo builds its own shim

| | |
|---|---|
| Status | accepted |
| Date | 2026-11-08 |
| Deciders | operator |
| Supersedes | [ADR-0001](0001-header-first-bindings.md) |

## Context

ADR-0001 made the shim generated infrastructure but left it in the **user's** build: `cppgo gen` wrote
`zz_cppgo_shim.cpp`/`.h` and the user's C++ build compiled them into the library the Go side loads.
The Go side stayed cgo-free, the API stayed complete, and the shim was parsed through libclang before
it was written. On that basis the shim workstream landed (PR #12, `5b4fef7`, verified against base
`67366f4`).

Three things then became true at once:

1. **The operator's constraint for #14**: everything is generated on the Go side, and the user's
   library is their own build, untouched by cppgo. Read alone, that forbids generated C++ anywhere.
2. **The clarification of 2026-11-08**: the constraint concerns the *user's build*. cppgo may generate and compile a shim **itself**, into an object of its own that links
   the user's library. The user writes no C++ and does not touch their build.
3. **The target libraries** (#15): OpenCV, ArrayFire and above all Eigen. Eigen is header-only and
   template-heavy, so the constructs that matter, instantiations, expression templates, by-value
   fixed-size matrices, are exactly the ones the direct ABI cannot express, and a user's build that
   has to compile a generated file is friction the product cannot afford.

Measured while answering #14 (all in this tree, `internal/callback` and `internal/shim`):

- C++→Go callbacks work with no cgo at build time, through `runtime.cgocallback`, **provided** the C
  call was made through `runtime.cgocall`: on the direct trampoline the same callback kills the
  process (`fatal error: morestack on g0`).
- a C++ exception with no C++ frame terminates the process, including through a Go callback frame.
- every construct the direct ABI cannot express (virtual dispatch, inline bodies, `std::` types,
  non-POD by value, templates, multiple inheritance, exception translation, `std::function`
  construction) is expressible in a C++ translation unit that cppgo compiles and links, with the
  user's library built separately and unchanged.

## Decision

**cppgo owns a third artefact.** For a bound header, cppgo emits and compiles `zz_cppgo_shim.cpp`
into `libcppgo_shim.so` (or a `.a`), linked against the user's library, and the generated Go package
loads the shim. The user's build and sources are never modified and never include generated code.

Everything else in ADR-0001's decision survives, with the carrier rules now measured:

- **two layers stay**: the runtime has no libclang. The generator has libclang and now also the C++
  compiler.
- **two carriers stay**, direct first. A mangled symbol the user's library exports, `noexcept`,
  non-virtual, register-representable, is called by a generated NOSPLIT trampoline. Everything else
  is a shim thunk.
- **a third call path is added**: a binding whose signature takes a function pointer or a
  `std::function` is called through `runtime.cgocall` (the *reentrant* carrier), because that is the
  only state in which the runtime accepts a callback.
- **the generated API keeps its shape**: `struct`/`NewXxx`/`Close`/methods, mirrorable PODs as Go
  structs, other records as opaque values, `cppgo.Handle` in no exported signature.
- **the manifest stays the command line** (`-bind`, `-exclude`, `-instantiate`, `-rename`,
  `-status`), and the primary path needs none of it.
- **the exception rule is now structural**: the shim catches, and a direct binding is `noexcept` by
  construction. There is no claim of exception support without a C++ frame.
- **templates are a two-phase flow**: the Go program instantiates the generic API, cppgo collects
  those instantiations, emits the matching C++ specialisations into the shim, compiles it, and the Go
  package calls the thunks. A Go instantiation with no C++ counterpart is a load-time error naming
  the line to add.

The v3 shim work from `5b4fef7` is reused as **semantics**. The by-value,
`std::`-marshalling and copy-back behaviour it measured is the acceptance oracle for the v4 thunks,
and its direct-carrier selection rule (an exported mangled name makes a header-only declaration
direct) is kept with its tests. The file it generated for the user's build is not part of v4.

## Consequences

Easier:

- the user's build gains nothing, which is what makes a header-only library like Eigen usable at all. There is no library, only the shim.
- exceptions, virtual dispatch, inline bodies, templates and `std::` types are supported by
  construction, because a C++ compiler is doing the work.
- the shim can be validated by compiling it, which is a stronger check than parsing it.

Harder, and stated plainly:

- **cppgo needs a C++ compiler**, and it must match the library's. Same family, same standard library,
  same `_GLIBCXX_USE_CXX11_ABI`. The generator records all three and rejects a mismatch.
- **Two objects ship instead of one**, and their load order matters. The shim links the library, so
  opening the shim brings the library in. A missing library is an `Open` error naming the shim and the
  library it needs.
- **A callback-capable binding is ~10× the cost of a direct one** (151 ns against 15.9 ns measured) and
  allocates 16 bytes per call, because the argument block cannot live on a goroutine stack a callback
  may grow.
- **A callback carries at most six INTEGER and eight SSE arguments**, and **an exception with no C++
  frame is fatal**. Both are measurements.
- **Determinism now spans a compiler.** The generated C++ is byte-identical for the same inputs, but
  the compiled shim is a build artefact: `cppgo check` diffs sources, and the shim's build is verified
  by compiling it, never by comparing binaries.

## Alternatives considered

- **Keep the shim in the user's build** (v3). Rejected by the operator's constraint. It makes a
  generated file a dependency of a build cppgo does not own, and it is the friction that rules out
  header-only template libraries.
- **Direct ABI only, refuse the rest.** Rejected in ADR-0001 and re-rejected here. Measured, it would
  exclude virtual dispatch, exceptions, `std::` types, by-value objects and every template.
- **Emit C++ into the user's tree but compile it ourselves with a generated makefile.** Same artefacts
  as this decision with an extra integration surface. The file lives in cppgo's output directory here.
- **A Go-side `std::` layout layer** (build `std::string` and `std::function` from documented layouts
  in Go). Measured to work only for types whose template instantiations the library happens to export.
  rejected as the primary mechanism, kept as the recorded fallback in `internal/callback`.
