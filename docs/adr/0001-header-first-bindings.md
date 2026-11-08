# ADR-0001: Bind C++ headers, and generate the shim that makes every construct bindable

| | |
|---|---|
| Status | accepted |
| Date | 2026-11-07 (revised the same day, see *Revision history*) |
| Deciders | operator |

## Context

v1 bound functions declared in a Go file: `//cppgo:external <symbol>` named the C symbol and a
hand-written `extern "C"` shim caught exceptions. v2 added a direct C++ ABI mode, still driven by Go
directives: `//cppgo:class mathx::Vector` on `type Vector cppgo.Handle`, `//cppgo:ctor`,
`//cppgo:method <name>`, `//cppgo:dtor`, plus an `//cppgo:abi` directive for what neither libclang
nor the binary could describe. Three tiers produced the same `cxx.Symbol`: clangd, inference from
the loaded `.so` by demangling, and the hand-written annotation.

Two problems were real:

1. **The Go declaration was the source of truth and could lie**, and every signature nobody declared
   (an overload, a constructor, a field) needed a hand-written Go mirror before it could be bound.
2. **The API showed the machinery**: `type Vector cppgo.Handle`, `New`/`Destroy`, and the same ABI
   lecture on every wrapper.

The first accepted decision of 2026-11-07 replaced the directive file with header-first generation
and made **generation-time validation a refusal**: an inline body, a virtual call, a `std::` type or
a by-value non-POD was a build error. The operator then corrected the posture:

> "se tiene que soportar todo, no vale rechazar", everything must be supported. An answer of rejection falls outside the product.

The correction is right on the product level. A binding generator that answers idiomatic C++ with "change your C++" falls short of completeness, and the constructs being refused, inline accessors,
virtual dispatch, `std::string`, by-value objects, are exactly what real headers are made of.

## Decision

`cppgo gen` reads **C++ headers** and emits a Go package **plus a C++ translation unit**,
`zz_cppgo_shim.cpp`/`.h`, containing one `extern "C"` thunk per binding the direct ABI cannot
express. The user's normal C++ build compiles that unit into the library the Go side loads. The Go
build still uses no cgo and no `import "C"`.

Every binding gets exactly one **carrier**:

- **direct** — the measured NOSPLIT trampoline calls the mangled C++ symbol. Reserved for
  `noexcept`, non-virtual, out-of-line functions whose arguments fit the register rules. This is the
  fast path.
- **thunk** — the generated C++ function calls the target as C++: inline bodies, virtual methods
  (real dispatch), template instantiations, `std::` types, by-value non-PODs, `this` adjustment for
  multiple and virtual inheritance, exception catching, and argument blocks when registers run out.

The shim is **generated infrastructure**. It is derived from the
same header parse as the Go API, it is parsed back through libclang before it is written, and the
Go side resolves every one of its symbols at `Open`. There is no user-written `extern "C"` layer and
no directive language.

Generated shapes stay. Class → Go struct with methods, `New<Type>(...) (*Type, error)`,
`Close() error`, mirrorable POD → Go struct with matching exported fields, other records → opaque
value types with `Close`, `Clone` and generated field accessors, free and static functions →
package functions, enums → named Go types, values in Go-native types, `cppgo.Handle` in no exported
signature.

The **binding manifest** is the command line: `-bind`/`-exclude` name roots, `-instantiate` names
the template specialisations the API uses, `-rename` resolves a Go-name collision, `-status` marks
the C++ status convention. These name declarations. They never describe a signature.

The runtime package at the repository root still owns only the C ABI and the loader. It never
imports `internal/cxx` and needs no libclang. `purego`, the NOSPLIT trampoline, `ProbeStack` and the
two-pass test contract survive unchanged.

## Consequences

Easier. Every construct in the bound API is reachable. A header needs no rewriting into "bindable" C++. Virtual dispatch is real. Exceptions become Go errors. `std::` containers and smart
pointers have stated ownership. The user sees one ordinary Go package.

The costs, stated plainly:

- **The build gains a C++ translation unit.** The generated shim has to be compiled with the
  library, with the project's flags. Forgetting it is a load-time error naming the file, not a
  crash: `Open` resolves every thunk before anything runs.
- **The direct path is narrower than "the ABI".** A non-`noexcept` function cannot be called
  directly (there is no handler above a NOSPLIT trampoline), so it goes through a thunk even when
  its signature is register-representable. The measured 13.9 ns is the direct path. A thunk adds a
  C++ call frame. By-value aggregates have no size ceiling. The thunk copies them as arguments and
  a result above 16 bytes is written through the sret pointer into Go storage.
- **Error signatures follow exceptions.** A thunk returns an error when the call can throw or its
  marshalling allocates. A direct call returns none. The contract states the exact rule so the
  generated signatures are predictable.
- **Some inputs must be named by the user**, because the C++ leaves them unstated. Which template
  specialisation to build (`-instantiate`) and which Go name to use when two C++ names collide
  (`-rename`). That is input. The construct itself is supported.
- **A template specialisation is read from the most specific body there is**: a full explicit
  specialisation's own definition, else the best matching partial specialisation, else the primary.
  An instantiation with no body of its own, an implicit instantiation, or an explicit
  instantiation such as `template struct Box<int>;`, is synthesised (`Record.Synthesized`), and a
  layout is reported only when libclang completes the type (`Record.LayoutKnown`). The
  specialisation is a member-bearing record either way. When the layout is unknown its Go value is
  opaque, with generated field accessors. The same reading covers conversion
  operators, operators, static data members and nested types, records and enums alike. A member
  function template is bound only for a named instantiation, and its pattern alone is an input the
  manifest completes.
- **The binary-inference tier no longer produces bindings.** It is kept as the ELF cross-check for
  direct symbols. The generator emits no unvalidated binding.
- The generator requires libclang at generation time (it already did), and its golden tests must
  compile the generated shim with a real C++ compiler.

`long double` and `__int128` remain outside the type map (no Go value type). The fix is a wrapper
with a representable signature, which is a one-line C++ change with no manual binding layer.
C++→Go callbacks are a separate direction and a separate feature. They fill a different gap.

## Revision history

- 2026-11-07, first accepted. Header-first generation with refusal for constructs the direct ABI
  could not express, and the shim mode retired.
- 2026-11-07, revised. The operator's correction replaced refusal with a generated shim and made
  completeness the product. This file was rewritten in place rather than superseded because the
  first version had not landed on `main`. From its merge onward, a change of this decision is a new
  ADR that supersedes this one.
