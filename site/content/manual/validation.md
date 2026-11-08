+++
title = "Validation and the report"
description = "What cppgo gen checks before it emits anything, where the compilation flags come from, every construct the build refuses today, how a shim compile error is tied back to its binding, and how to read the carrier report."
weight = 80
+++

**Audience:** a run that failed, or a report you are reading before trusting the
package.

## Why validation exists

A generated call is a promise that a symbol exists and that its arguments fit
the carrier. `cppgo gen` establishes both before it emits anything: the header
parse supplies types, sizes, `const`-ness, virtual-ness and the exception
specification, and — when `-library` is a readable file — the library's dynamic
symbol table proves that a direct binding's name is really exported.

The generator never leaves a declaration unbound by accident. Every binding gets
a carrier, or the run fails. A silent third state is the bug this page exists to
rule out.

## Where the compilation flags come from

A header only means something with the flags it is compiled with. cppgo takes
them from clangd's configuration instead of inventing them:

1. `-compile-commands <path>` when given (a `compile_commands.json` or its
   directory), else the database `.clangd` names, else the nearest
   `compile_commands.json` walking up from the header;
2. `.clangd` files beside the header and in its ancestors, merged as clangd
   does;
3. otherwise `-std=c++17 -I<header dir>`, **reported as an assumption**.

When the flags are the fallback, a warning goes to stderr:

```
cppgo: warning: the compilation flags for /src/mathx/vector.hpp are an
assumption (assumed: -std=c++17 -I/src/mathx); a parse without the project's
real flags can misread the header
```

That warning is serious for a header with its own includes: a failed include
means libclang sees a partial declaration, and the validation is only as good as
what it saw. Point `-compile-commands` at your build's database.

## Reading the report

One line per binding on stdout, ordered by qualified signature, then the shim
build:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, out of line, not virtual, register class
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

The columns are:

1. **the C++ signature**, with `const` and `noexcept` where the header has them;
2. **the carrier** — `direct`, `thunk` or `reentrant`;
3. **the symbol** the binding reaches: a mangled name of your library for a
   direct binding, a generated `cppgo_thunk_<hash>` for a shim binding;
4. **the reason** the carrier was chosen.

Read the carrier and the reason together. `direct` with
`noexcept, out of line, not virtual, register class` is the fast path;
`thunk` with `not noexcept: an exception would unwind into Go` is the safe one.
The hash is stable for a given qualified signature, so a diff in it means the
signature changed.

When `-library` is a readable file, a direct binding's evidence comment ends
`, verified against libname.so` in the generated file. That is the ELF
cross-check, and it is why a header-only declaration whose symbol happens to be
exported can still be direct.

## What this build refuses

Each of these is a generation error that writes nothing. The message names the
file, the line, the declaration, what is wrong, and what to do — the shape is
`cppgo: <file>:<line>: <declaration>: <what is wrong>; <fix>`.

| input | message (short form) | what to do |
|---|---|---|
| an **enum**, anywhere in the parsed headers | *enums are not emitted by this build yet; pass the value as its underlying integer at the C++ boundary* | use an integer at the boundary |
| a **class template** with no `-instantiate` | *a class template is not a type until it is instantiated; name a concrete instantiation with `-instantiate 'Box<...>'`* | add `-instantiate` |
| a **type nested** inside a record | *a type nested inside a record is not emitted by this build yet; move the nested type to namespace scope…* | move it to namespace scope |
| **inheritance** (any base) | *inherited members and base views are not emitted by this build yet; bind the base class directly…* | bind the base, or wrap it |
| a **static data member** | *the static data member X is not emitted by this build yet; expose it through a static member function or a free function* | expose a function |
| a **member function template** | *the member function template X is not emitted by this build yet; expose the concrete instantiation as a non-template member* | expose a concrete member |
| a **free function template** | *a function template has no symbol until it is instantiated; expose a concrete instantiation as a non-template function* | expose a concrete function |
| an **operator** or **conversion operator** | *operators and conversion operators are not emitted by this build yet; give it a named member function or a named free function* | name it |
| a **variadic** function | *a variadic function has no fixed argument list to lay out; replace the ellipsis with a fixed parameter list or a span* | take a slice or a fixed list |
| `long double` / `__int128` | the parameter or result, and the fix | widen or narrow at the boundary |
| a callback with more than six integer or eight floating-point arguments | the sixth argument | take a pointer to a struct |
| a macro that is the whole API | the macro, which libclang cannot see as a declaration | write a function |
| `-bind` / `-exclude` that names nothing | *it names nothing this header declares* | fix the name |
| `-instantiate` that names no visible instantiation | *it names no instantiation this header declares; add an explicit instantiation such as `template struct ...;` to the header* | make it visible |
| `-rename` that matches nothing, or several declarations | both declarations | name the full signature |
| `-status` on a non-32-bit, non-integer return | the type it found | name a function that returns `int32_t` |

The current refusal set is deliberately broad: it is checked **before**
selection, so a header that declares any enum anywhere fails even if you did not
bind it. That is a limitation of this phase, not a policy; the constructs are
part of the design and their turn comes after the v4 epic.

## A shim that does not compile

The shim is C++ that cppgo compiles itself. A compile error is a generation
error carrying the compiler's diagnostic, the file and line, and the binding
whose thunk is in it. The shape is:

```
cppgo: <file>:<line>: <declaration>: the generated shim does not compile; <compiler diagnostic> (compiler: <the argv that ran>)
```

On that path only, cppgo compiles one thunk at a time to find which binding the
diagnostic belongs to, so a header that the shim cannot include still produces
an error naming the declaration instead of a wall of compiler output.

## Determinism and `check`

The generated Go and C++ are byte-identical for the same inputs: bindings are
sorted by qualified name and thunk names come from a stable hash of the
qualified signature. `cppgo check` is the gate:

```console
$ cppgo check -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx: generated files are up to date
```

It regenerates in memory and compares every source byte for byte. It does not
compile the shim, because the shim is a build artefact; the way to verify it is
to build it, which `cppgo gen` does on every run.

Next: [cppgo check in CI]({{< ref "manual/ci.md" >}}).
