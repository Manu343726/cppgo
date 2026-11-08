---
title: "Generate a binding for your own library"
description: "The command, the compilation configuration, and what a generation error looks like."
weight: 10
---

You have a C++ library with headers and you want a Go package that calls it.
This guide assumes you have read
[Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}}) or already
understand what `cppgo gen` produces.

## 1. State how the header is compiled

`cppgo gen` parses your headers with libclang. Libclang sees the program the
compiler would see only when it is given the same include paths and the same
language standard, so the first thing to supply is that configuration. There are
two ways, in this order of preference:

- **A compilation database.** CMake, Meson and most large projects produce
  `compile_commands.json`. Point at it and cppgo takes the flags of the entry
  that covers each header:

  ```console
  $ cppgo gen -header include/mylib/api.hpp -package mylib \
              -compile-commands build/compile_commands.json -out ./mylib
  ```

  cppgo rebases the database's relative include paths against the entry's own
  `directory`. It has to: libclang resolves a relative path against *cppgo's*
  working directory, which is a different tree, and the parse then succeeds
  while seeing a different program. `TestRebaseArgs` in `internal/gen` pins the
  rebase.

- **A `.clangd` file.** For a project with no compilation database, the
  `CompileFlags` block in `.clangd` is enough. libclang reads the include paths
  and the standard, and the same file keeps clangd's editor behaviour correct:

  ```yaml
  CompileFlags:
    Add: [-std=c++20, -Iinclude]
  ```

Do not skip this because the header compiles: the generator needs the flags, and
a missing include path produces a binding for a declaration it could see, not an
error. When it finds no configuration at all, cppgo says so and assumes a
default, because a parse without the project's real flags can misread the
header:

```
cppgo: warning: the compilation flags for api.hpp are an assumption
(assumed: -std=c++17 -Iinclude); a parse without the project's real flags can
misread the header
```

## 2. Decide what to bind

By default, the header is the unit of selection: every public declaration in it
is bound, plus the type closure of every bound signature. For a large library,
narrow the set with roots and exclusions:

```console
$ cppgo gen -header include/opencv2/core.hpp -package core \
            -bind cv::Mat -bind 'cv::cvtColor' -exclude 'cv::detail' -out ./cvcore
```

A root is a namespace, a class, a member or a function. An entry that names
nothing is an error, never a silent no-op:

```console
cppgo: -bind cv::Maat: it names nothing this header declares
```

That is deliberate. A typo that silently shrinks the API is the failure mode
worth an error, because nobody notices a missing function until it is needed.

## 3. Run it

```console
$ cppgo gen -header include/mylib/api.hpp -package mylib \
            -library build/libmylib.so -out ./mylib
```

| Flag | What it does |
|---|---|
| `-header <path>` | the C++ header to bind; repeatable, and each header is a selection unit |
| `-package <name>` | the Go package name written into `zz_cppgo.gen.go` |
| `-out <dir>` | where the package and the shim are written (default `.`) |
| `-library <file>` | your shared object: the shim links against it and its `.dynsym` cross-checks every direct binding. **Omit it for a header-only library** |
| `-compile-commands <path>` | the database, or its directory |
| `-bind`, `-exclude` | add or remove a root |
| `-rename 'sig=GoName'` | resolve a Go name collision |
| `-instantiate '<specialization>'` | name a concrete template specialisation |
| `-status <qualified>` | a declaration whose `int32` return is a status code |
| `-cxx <compiler>` | the compiler for the shim (default `g++`, then `clang++`) |
| `-check` | report staleness instead of writing |
| `-q` | suppress the report |

The complete reference is [Command line]({{< ref "reference/cli.md" >}}).

`-library` is a path to the object, not a name the linker should search for. With
a path, cppgo links the shim against the library's soname (or its file name) and
cross-checks direct bindings against the library's dynamic symbol table. With no
library, the shim is the whole binary, which is the header-only case.

## 4. Read the report

The report is the audit of what was bound and how. Every line names one C++
declaration, its carrier, its symbol and the reason for that carrier:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

When a direct binding was cross-checked, the generated comment ends
`, verified against libmathx.so`. When the shim is built, the last line names
the compiler that built it. That line matters: the shim must be built with the
same compiler family and the same standard library as your library, so seeing
the wrong compiler there is the earliest warning you get.

## 5. Commit the sources and check them

The generated Go, the assembly and the shim sources are reproducible: the same
inputs produce byte-identical output, bindings are sorted by qualified name, and
a thunk's name is a stable hash of the qualified signature. Commit them, and make
the regeneration command checkable:

```console
$ cppgo check -header include/mylib/api.hpp -package mylib \
              -library build/libmylib.so -out ./mylib
./mylib: generated files are up to date
$ echo $?
0
```

`cppgo check` regenerates in memory, compares with the files on disk, writes
nothing and builds nothing. It exits `0` when everything matches, `1` when a file
is missing or stale, and `2` when the command line itself is wrong. The exact
invocation is written into the banner of `zz_cppgo.gen.go`, so the command that
produced a file lives beside it.

[Gate a generated package in CI]({{< ref "how-to/ci.md" >}}) puts the check in a
pipeline.

## When it refuses

A generation error names the file, the line, the declaration and the fix, and
nothing is written. These are the ones you are most likely to meet:

| Message contains | What happened | What to do |
|---|---|---|
| `it names nothing this header declares` | a `-bind`, `-exclude`, `-rename` or `-status` entry matches no declaration | fix the qualified name or the signature |
| `not noexcept: an exception would unwind into Go` (in the report, not an error) | the call can throw; it goes through the shim | nothing, unless you expected a direct call |
| `operators and conversion operators are not emitted by this build yet` | the declaration is an operator | give it a named member or free function |
| `enums are not emitted by this build yet` | the header declares an enum | pass the value as its underlying integer at the C++ boundary |
| `inherited members and base views are not emitted by this build yet` | a class has base classes | bind the base directly, or expose the derived members through a wrapper |
| `the call needs N INTEGER and M SSE argument registers` | more arguments than the registers carry, and the argument block is not implemented | take a pointer to a struct |
| `no Go value type` | `long double`, `__int128` or `nullptr_t` in a signature | wrap it at the C++ boundary |
| `the shim ... does not compile` | the generated C++ failed to compile with your flags | the error carries the compiler's diagnostic |

The full list, with the test that pins each one, is
[Limits]({{< ref "explanation/limits.md" >}}).

If a construct you need is in that list, it is not a flag you have missed: it is
work this build has not done. The list is `rejectUnimplemented` in
`internal/gen/reject.go`, and every entry names the construct and the fix.

## Related

- A template-heavy library: [Bind a C++ template library]({{< ref "how-to/templates.md" >}}).
- What the generated package looks like: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Moving the result to a deployment: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
