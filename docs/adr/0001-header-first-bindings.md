# ADR-0001: Bind C++ headers, and generate the shim that makes every construct bindable

| | |
|---|---|
| Status | accepted; its shim-ownership clause is superseded by [ADR-0002](0002-cppgo-built-shim.md) |
| Date | 2026-11-07 (revised the same day, see *Revision history*) |
| Deciders | operator |

## Context

The question this record answers is **what a binding generator reads**: a description the user
writes, or the C++ header itself. cppgo answered it the other way round twice before.

**v1** bound functions declared in a Go file. A `//cppgo:external <symbol>` comment named the C
symbol to look up, and a hand-written `extern "C"` shim caught exceptions on the way back. *C
linkage* (`extern "C"`) is the C++ declaration form whose symbol keeps its plain name instead of
being mangled — the compiler's encoding of namespaces, classes and overloads into one symbol name —
so a `dlsym` lookup can find it.

**v2** added a direct call into the C++ ABI, still driven by Go comments:

- `//cppgo:class mathx::Vector` on `type Vector cppgo.Handle` said which C++ class a Go type stood
  for, `cppgo.Handle` being an opaque `uintptr` that was all the Go side knew of the object.
- `//cppgo:ctor`, `//cppgo:method <name>` and `//cppgo:dtor` listed the members to bind.
- `//cppgo:abi` carried whatever neither *libclang* — the C library of the Clang compiler front end,
  which answers questions about a C++ declaration without compiling it — nor the compiled object
  could describe.

Three sources produced the same binding: clangd, inference from the loaded `.so` by demangling its
symbols (recovering namespaces and parameter types from the encoded name), and the hand-written
annotation.

Two problems were real:

1. **The Go declaration was the source of truth and could lie**, and every signature nobody declared
   (an overload, a constructor, a field) needed a hand-written Go mirror before it could be bound.
2. **The API showed the machinery**: `type Vector cppgo.Handle`, `New`/`Destroy`, and the same
   lecture about the ABI on every wrapper.

The first accepted decision of 2026-11-07 replaced the directive file with header-first generation
and made **generation-time validation a refusal**: an inline body, a virtual call, a `std::` type, or
a by-value non-POD (a class with a user-declared constructor, destructor or copy operation, whose
bytes therefore cannot be copied as they are) was a build error. The operator then corrected the
posture:

> "Everything has to be supported; refusing is not acceptable."
>
> — the operator, translated from Spanish

The correction is right on the product level. A binding generator that answers idiomatic C++ with
"change your C++" falls short of completeness, and the constructs being refused — inline accessors,
virtual dispatch, `std::string`, by-value objects — are exactly what real headers are made of.

## Decision

`cppgo gen` reads **C++ headers** and emits a Go package **plus a C++ translation unit**,
`zz_cppgo_shim.cpp`/`.h`, containing one `extern "C"` thunk per binding the direct ABI cannot
express. A *thunk* is a small `extern "C"` function whose body calls one C++ declaration the way C++
itself would. The user's normal C++ build compiles that unit into the library the Go side loads. The
Go build still uses no *cgo* — Go's own way of calling C, which needs a C toolchain at build time —
and no `import "C"`.

Every binding gets exactly one **carrier**, the mechanism that performs the call:

- **direct** — the measured NOSPLIT trampoline calls the mangled C++ symbol. `NOSPLIT` is the Go
  assembler flag that removes the stack-growth preamble, so the function may not grow the stack and
  the runtime never preempts it asynchronously. This carrier is reserved for `noexcept`, non-virtual,
  out-of-line functions whose arguments fit the register rules. It is the fast path.
- **thunk** — the generated C++ function calls the target as C++: inline bodies, virtual methods
  (real dispatch through the object's vtable, the per-class array of function pointers), template
  instantiations, `std::` types, by-value non-PODs, `this` adjustment for multiple and virtual
  inheritance, exception catching, and argument blocks when the argument registers run out.

The shim is **generated infrastructure**. It is derived from the same header parse as the Go API, it
is parsed back through libclang before it is written, and the Go side resolves every one of its
symbols at `Open`. There is no user-written `extern "C"` layer and no directive language.

Generated shapes stay. Class → Go struct with methods, `New<Type>(...) (*Type, error)`,
`Close() error`; a mirrorable POD → a Go struct with matching exported fields; every other record →
an opaque value type with `Close`, `Clone` and generated field accessors; free and static functions →
package functions; enums → named Go types; values in Go-native types; and `cppgo.Handle` in no
exported signature.

The **binding manifest** is the command line: `-bind`/`-exclude` name roots, `-instantiate` names
the template specialisations the API uses, `-rename` resolves a Go-name collision, and `-status`
marks the C++ status convention — a plain integer return code. These name declarations. They never
describe a signature.

The runtime package at the repository root still owns only the C ABI and the loader. It never
imports `internal/cxx` and needs no libclang. `purego`, the NOSPLIT trampoline, `ProbeStack` — the
call that guarantees a fixed amount of goroutine stack headroom — and the two-pass test contract
survive unchanged.

## Consequences

Easier. Every construct in the bound API is reachable. A header needs no rewriting into "bindable"
C++. Virtual dispatch is real. Exceptions become Go errors. `std::` containers and smart pointers
have stated ownership. The user sees one ordinary Go package.

The costs, stated plainly:

- **The build gains a C++ translation unit.** The generated shim has to be compiled with the
  library, with the project's flags. Forgetting it is a load-time error naming the file, not a
  crash: `Open` resolves every thunk before anything runs.
- **The direct path is narrower than "the ABI".** A non-`noexcept` function cannot be called
  directly (there is no handler above a NOSPLIT trampoline), so it goes through a thunk even when
  its signature is register-representable. The measured 13.9 ns is the direct path. A thunk adds a
  C++ call frame. By-value aggregates have no size ceiling: the thunk copies them as arguments, and
  a result above 16 bytes is written through the hidden *sret* pointer — the pointer a caller
  supplies for a result returned through memory — into Go storage.
- **Error signatures follow exceptions.** A thunk returns an error when the call can throw or its
  marshalling allocates. A direct call returns none. The contract states the exact rule so the
  generated signatures are predictable.
- **Some inputs must be named by the user**, because the C++ leaves them unstated: which template
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
  direct symbols — a comparison against the library's dynamic symbol table. The generator emits no
  unvalidated binding.
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
- 2026-11-08, partly superseded. [ADR-0002](0002-cppgo-built-shim.md) keeps every decision here
  except one: the user's build no longer compiles the shim; cppgo compiles it itself.
