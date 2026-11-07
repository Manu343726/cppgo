# ADR-0001: Bind C++ headers directly, and retire the directive DSL and the shim mode

- Status: accepted
- Date: 2026-11-07
- Deciders: operator

## Context

v1 bound functions declared in a Go file: `//cppgo:external <symbol>` named the C symbol and a
generated `extern "C"` shim caught exceptions. v2 added a direct C++ ABI mode, still driven by Go
directives: `//cppgo:class mathx::Vector` on `type Vector cppgo.Handle`, `//cppgo:ctor`,
`//cppgo:method <name>`, `//cppgo:dtor`, plus an `//cppgo:abi` directive for the cases where neither
libclang nor the binary could describe the call. Three tiers existed side by side — clangd,
inference from the loaded `.so` by demangling the mangled name, and the hand-written directive — and
all three produced the same `cxx.Symbol`.

Three problems accumulated:

1. **The Go declaration was the source of truth and could lie.** The directive describes a
   signature; libclang's job was then to check it. Every mismatch needed a handwritten error path,
   and a signature nobody declared (a constructor overload, a field) could not be bound at all
   without first writing a Go mirror.
2. **The shim and the annotation tier were unvalidated paths.** What the direct ABI could not
   express was pushed through a second, hand-written C-like layer, or asserted into existence. The
   generated API therefore contained bindings the generator could not prove, which is the one thing
   this project exists to prevent.
3. **The API showed the machinery.** `type Vector cppgo.Handle`, `New`/`Destroy`, and a repeated
   ABI comment on every wrapper made generated code read like a binding, not like a Go package.

## Decision

`cppgo gen` reads **C++ headers** and emits a Go package. It never reads a Go file, never rewrites
one, and has no directive language. Everything it emits is derived from the C++ declaration
(libclang, with the project's `compile_commands.json`/`.clangd` flags) and cross-checked against the
built library's dynamic symbol table when the library is available.

Generated shapes: class → Go struct with methods, constructor → `New<Type>(...) (*Type, error)`,
destructor → `Close() error`, POD struct → Go struct with matching exported fields, free and static
functions → package functions, enums → named Go types, values in Go-native types. `cppgo.Handle`
appears in no exported signature.

The shim mode, the Go directive DSL, the hand-written ABI tier and `-force` are removed. A binding
that cannot be expressed is a build error naming the declaration and the fix.

The runtime package at the repository root keeps owning only the ABI and the loader; it never
imports `internal/cxx` and needs no libclang. `purego`, the NOSPLIT trampoline, `ProbeStack` and the
two-pass test contract survive unchanged.

## Consequences

Easier: one source of truth; no mirror file to maintain; every binding validated; the generated
package is ordinary Go; a header change that is not regenerated fails the build (compile-time layout
assertions) instead of a call.

Harder, and accepted: a library author who wants a Go binding must make the API bindable — members
defined out of line, no `std::` types in bound signatures, no by-value aggregates above 16 bytes,
no exceptions escaping. There is no escape hatch for what C++ can express and the ABI mode cannot:
the answer is a plain, non-inline C++ function taking pointers and lengths, which remains
expressible and validated. The binary-inference tier can no longer produce bindings; it is kept only
as a cross-check, so "no headers" is no longer a supported input. The generator now requires
libclang at generation time (it already did) and its golden tests must regenerate through a real
header parse.

`Handle` survives in the runtime as a low-level type for hand-written callers, but generated code
does not use it.
