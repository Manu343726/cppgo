+++
title = "Troubleshooting"
description = "The failures you will actually hit: libclang missing, a shim that does not compile, a construct this build refuses, a manifest entry that names nothing, and a load failure at the first call."
weight = 100
+++

**Audience:** a run failed and you need the cause, or a program panicked on its
first call.

## libclang not found

**Symptom**

```
cppgo gen: cppgo: include/mathx/vector.hpp: cxx: libclang not found (searched
libclang.so, /usr/lib/libclang.so, ...; install libclang-dev or set CPPGO_LIBCLANG)
```

**Why.** The generator parses the header with libclang. Without it there is
no declaration to read. The run finds nothing to parse and fails instead of guessing.

**Fix.** Install your distribution's libclang development package (the message
suggests `libclang-dev`), or set `CPPGO_LIBCLANG=/path/to/libclang.so`.

## The generated shim does not compile

**Symptom**

```
cppgo: <file>:<line>: <declaration>: the generated shim does not compile;
<compiler diagnostic> (compiler: <argv>)
```

**Why.** The shim is C++ that cppgo compiles with the flags it discovered for
the header. The parse can succeed while the compile fails, because the two use
different flag sets or a different standard, or because the header depends on an include the translation unit lacks.

**Fix, in order:**

1. Pass the real compilation database: `-compile-commands build`. That is what
   puts the project's include paths and defines on the shim's command line.
2. Make sure the compiler matches the library's: `-cxx clang++` (or `g++`), and
   a standard the header accepts. cppgo adds `-std=c++17` only when the
   discovered flags name no standard.
3. If the diagnostic names a member the shim cannot touch (a private destructor,
   a deleted copy constructor), the header is exposing something a caller cannot
   use either. Expose a public wrapper. The generator binds what a caller can
   call.

The error names the declaration because cppgo re-compiles one thunk at a time on
the failure path. The diagnostic is the compiler's, not a summary.

## A construct this build refuses

**Symptom.** A one-line error naming the file, the declaration, and the fix:

```
cppgo: /src/cell.hpp:9: tmpl::Cell: a class template is not a type until it is
instantiated; name a concrete instantiation with -instantiate 'tmpl::Cell<...>'
```

**Why.** The construct has no representation in the emitter yet. It is a
refusal, not a bad declaration. The run writes no output, and no later call can fail.

**Fix.** The second half of the message says what to supply:

| message contains | cause | answer |
|---|---|---|
| *enums are not emitted* | an enum anywhere in the parsed headers | pass the value as its underlying integer |
| *a class template is not a type* | no `-instantiate` | name the specialisation |
| *a type nested inside a record* | a nested type | move it to namespace scope |
| *inherited members and base views* | a base class | bind the base directly, or wrap it |
| *static data member* | a static field | expose a static function |
| *member function template* | a member template | expose a concrete member |
| *a function template has no symbol* | a free template | expose a concrete function |
| *operators and conversion operators* | `operator+`, `operator bool` | give it a name |
| *a variadic function* | `...` | a fixed list or a span |

One consequence surprises people. The refusal is checked **before selection**,
so a header that declares an enum anywhere fails even when you `-exclude` it.
Split the header, or wait for the construct.

The full list is in [validation]({{< ref "manual/validation.md" >}}).

## A manifest entry that names nothing

**Symptom**

```
cppgo: -bind cv::Mat: it names nothing this header declares
```

**Why.** `-bind`, `-exclude`, `-instantiate`, `-rename` and `-status` are
checked against what the parse actually saw. A typo must not silently shrink the
API. An unmatched entry is an error.

**Fix.** Check the qualified name against the header. A common cause is a
namespace alias, or a declaration in a system header that libclang did not
expose as public. `-rename` may also need the full signature when a qualified
name matches several overloads.

## `a direct binding needs -library`

**Symptom**

```
cppgo: <declaration>: a direct binding needs -library: name the shared object
its symbol is exported by
```

**Why.** A declaration the header shows as out of line is routed to the direct
carrier, and the proof that the symbol exists comes from the library's dynamic
symbol table. Without `-library`, cppgo cannot check it.

**Fix.** Pass `-library build/libyourlib.so`. For a header-only library there is
no symbol. There is no direct binding to check. Omit `-library` and the whole
package is shim-carried.

## The first call panics

**Symptom.** A panic from a binding that has no `error` result, on the first
call, naming the shim path or a symbol.

**Why.** A generated package loads its two objects on the first call. A binding
with an `error` result returns the failure. One without an error result has
nowhere to put it. It panics. That is deliberate. The program was built
against a library that is absent.

**Fix.** Read the message. It names what failed:

| message names | cause | fix |
|---|---|---|
| the shim path | `Open` was given a path that does not exist, or `Open("")` and the shim is not on the loader's path | call `Open(".../libcppgo_<pkg>.so")`, or set `LD_LIBRARY_PATH` |
| the user library's soname | the loader cannot find it | keep it beside the shim, or set `CPPGO_LIBRARY_PATH` / `CppgoLibraryPath` |
| a thunk symbol, with *the shim cppgo compiled is stale; run cppgo gen again* | the `.so` beside the package is not the one the generated Go was built against | regenerate; the two are one artefact pair |
| a symbol of your library | `-library` pointed at the wrong object, or the symbol is not exported | check `nm -D --defined-only build/libyourlib.so` |

## Wrong compilation flags

**Symptom.** The assumed-flags warning, or a declaration refused that you know
is different:

```
cppgo: warning: the compilation flags for /src/mathx/vector.hpp are an
assumption (assumed: -std=c++17 -I/src/mathx); a parse without the project's
real flags can misread the header
```

**Why.** The header was parsed without the include paths, defines or standard
the build uses. libclang then sees a partial or wrong declaration, sometimes
none at all, which reads as *it names nothing this header declares*.

**Fix.** Generate a `compile_commands.json` (`cmake
-DCMAKE_EXPORT_COMPILE_COMMANDS=ON`, or `bear -- make`) and pass it:

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -compile-commands build -out ./mathx
```

or put a `.clangd` next to the header:

```yaml
CompileFlags:
  CompilationDatabase: build
  Add: [-DMATHX_USE_DOUBLES]
```

Then check that the warning is gone. The warning is the only signal that the
parse may be missing something. A successful run without it is the normal state.

## When in doubt, ask the report

The per-binding report is the tool explaining itself. Before changing code, run
`cppgo gen` without `-q` and read:

1. **the carrier** — `direct` means a trampoline calls your symbol. `thunk`
   means the shim does, which is where an inline body, a `std::` type or an
   exception is handled. `reentrant` means a callback was involved.
2. **the symbol** — a mangled name of your library, or a stable
   `cppgo_thunk_<hash>`.
3. **the reason** — the sentence that decided the carrier.

If the reason is *not noexcept*, the signature has an error because the thunk
can report a caught exception. If it is *inline body*, the thunk is the only
carrier that can reach it. Neither is a problem. They explain the API you see.

Back to the [manual]({{< ref "manual/_index.md" >}}), or read [when generation
refuses]({{< ref "tutorials/when-generation-refuses.md" >}}) for the decision
procedure in narrative form.
