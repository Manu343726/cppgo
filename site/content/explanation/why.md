---
title: "Why cppgo exists"
description: "What cgo and hand-written bindings cost, and what cppgo does instead."
weight: 10
---

Calling C++ from Go is possible today, and each existing way costs something
specific. cppgo exists to avoid two of those costs at once: the cost of cgo in
the build, and the cost of hand-writing the interface to a C++ library.

## The two usual options

**cgo.** Go can call C through cgo: import `"C"`, write the C declarations in a
comment, and the toolchain builds a C wrapper for you. Two problems follow. The
first is that C++ does not cross a C boundary: classes, methods, templates,
exceptions and `std::` types have to be wrapped by hand in `extern "C"` functions
— C++ functions with C linkage, which gives them the C calling convention and a
name that is not mangled — so you end up writing the binding anyway, one wrapper
per construct. A *binding* is one C++ declaration turned into a callable Go
function; writing one by hand means writing that wrapper. The second problem is
the build: cgo means a C compiler in the Go build, and the package becomes harder
to cross-compile and to reason about.

The per-call overhead is the other half. On the same call, reaching a `noexcept`
C++ function — one that promises never to throw — through the generated assembly
instead of through a C wrapper is about 2.8 times faster than cgo: 10.5 ns
against 29.6 ns, with neither allocating.
[Performance]({{< ref "explanation/benchmarks.md" >}}) has the table, the ranges
those medians come from, and the paths where cppgo is currently slower.

**A hand-written binding over a foreign-function library.** A *foreign-function
library* is a Go package such as [`purego`](https://github.com/ebitengine/purego)
that calls a native function at an address it looked up itself, with no cgo.
Using one means writing the declaration yourself: the mangled symbol (the name
the C++ compiler encodes on the symbol, with the parameter types folded into it),
the argument layout, the calling convention (which registers and which memory
carry the arguments), the ownership rules. That removes cgo, and it moves the
whole interface into a Go file that nothing checks against the header. When the
header changes, the binding keeps compiling and starts calling the wrong thing.

## What cppgo does differently

cppgo reads the header and writes the binding. The header is the source of
truth, and the Go API is derived from it:

```console
$ cppgo gen -header include/user.hpp -package user -library libuser.so -out ./user
```

Three words recur in what follows, so they are defined here. A **trampoline** is
a few instructions of generated Go assembly that jump to a C++ function. A
**shim** is a C++ file that cppgo generates and compiles, holding the C++ code a
plain register call cannot express. The **ABI** — *application binary interface*
— is how a compiled call is laid out: which registers and which memory carry its
arguments and its result.

The properties that follow are the whole design:

1. **The API mirrors the C++.** A class becomes a Go struct with methods; a
   constructor (the C++ function that creates an object) becomes `New<Type>`; the
   destructor (the one that runs when the object is destroyed) becomes `Close`; a
   free function becomes a package function; and an overload — two C++ functions
   with one name and different parameter types — keeps a parameter-type suffix.
   The caller does not see the machinery: no handle type, no ABI parameters (no
   extra arguments that exist only to satisfy the calling convention), no
   macro-generated stubs.

2. **The C++ side is generated too.** The C ABI — the calling surface a C
   compiler produces, with no classes, no exceptions and no name mangling —
   cannot express inline bodies, virtual dispatch (a call resolved at run time
   from the object's type), `std::` types, by-value objects, templates or
   exception translation (a C++ exception turned into a Go error). Those are
   carried by a C++ translation unit that cppgo writes and compiles itself. That
   is what makes "everything is supported" a statement about a mechanism rather
   than a slogan.

3. **Your build is untouched.** The library is built by your build, with your
   flags, and cppgo links its shim against it from the outside. No generated file
   is compiled into your library. For a header-only library there is no library
   at all: the shim is the whole binary.

## What it buys, and what it costs

Easier:

- you write no binding declarations and no `extern "C"` layer;
- your build gains no step, so a header-only, template-heavy library works;
- the generated package is ordinary Go, with no cgo;
- exceptions become Go errors, `std::` types have stated ownership, and integer
  and pointer arguments cross without a copy.

Harder, and stated plainly:

- **generation needs libclang and a C++ compiler** — libclang being the C
  library of the Clang compiler front end, which parses a header the way the
  compiler would — because reading the header and building the C++ side are part
  of the pipeline;
- **two files ship** instead of one — the shim, and the library it links against
  — and their relative position matters;
- **the generator has to model C++**, and where it does not yet, it refuses
  rather than guesses — see [Limits]({{< ref "explanation/limits.md" >}}). Today
  that refusal list includes enums, inheritance, operators and smart pointers;
- **the C++ side must match your library's ABI**: same compiler family, same
  standard library, same `_GLIBCXX_USE_CXX11_ABI` — the build macro that selects
  which of two incompatible `std::string` layouts the GNU standard library
  compiles. Nothing detects a mismatch for you: cppgo reports the compiler it
  used, and keeping the two in step is a build decision. Mixing them is not a
  build error; it is undefined behaviour at the first call.

## When cppgo is not the answer

- A C library with a stable C API: cgo or `purego` directly is simpler. cppgo's
  value is the C++ side.
- A one-off call into one function: writing one `syscall`/`purego` call is less
  work than installing libclang.
- A library whose API is a set of macros rather than functions: a macro is text
  substitution the preprocessor performs before compilation, and its expansion is
  not a declaration, so there is nothing for cppgo to read.

## Related

- How the pieces fit: [Architecture]({{< ref "explanation/architecture.md" >}}).
- What a call costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
- What the mechanisms stop at: [Limits]({{< ref "explanation/limits.md" >}}).
