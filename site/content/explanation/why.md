---
title: "Why cppgo exists"
description: "What cgo and hand-written bindings cost, and what the design buys instead."
weight: 10
---

Calling C++ from Go is possible today, and each existing way costs something
specific. cppgo exists to avoid two of those costs at once: the cost of cgo in
the build, and the cost of hand-writing the interface to a C++ library.

## The two usual options

**cgo.** Go can call C through cgo: import `"C"`, write the C declarations in a
comment, and the toolchain builds a C wrapper for you. Two problems follow. The
first is that C++ does not cross a C boundary: classes, methods, templates,
exceptions and `std::` types have to be wrapped in `extern "C"` functions by
hand, so you end up writing the binding anyway — in C, per construct. The second
is the build: cgo means a C compiler in the Go build, and the package becomes
harder to cross-compile and to reason about.

The per-call cost is measurable. A cgo call to a trivial function measures
105 ns/op against 2.1 ns/op for an indirect Go call, on the machine `DESIGN.md`
records (both 2026-11-03, CPU pinned to 964 MHz, so the ratio is the number to
read).

**A hand-written binding over a foreign-function library.** Packages such as
`purego` can call an address without cgo, and you write the declaration yourself:
the mangled symbol, the argument layout, the calling convention, the ownership
rules. That removes cgo, and it moves the whole interface into a Go file that
nothing checks against the header. When the header changes, the binding keeps
compiling and starts calling the wrong thing.

## What cppgo does differently

cppgo reads the header and writes the binding. The header is the source of
truth, and the Go API is derived from it:

```console
$ cppgo gen -header include/user.hpp -package user -library libuser.so -out ./user
```

Three properties follow, and they are the whole design:

1. **The API mirrors the C++.** A class becomes a Go struct with methods, a
   constructor becomes `New<Type>`, the destructor becomes `Close`, a free
   function becomes a package function, and an overload keeps a parameter-type
   suffix. The caller does not see the machinery: no handle type, no ABI
   parameters, no macro-generated stubs.

2. **The C++ side is generated too.** C++ constructs that cannot cross a C ABI
   — inline bodies, virtual dispatch, `std::` types, by-value objects, templates,
   exception translation — are carried by a C++ translation unit that cppgo
   writes and compiles itself. That is
   [ADR-0002](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0002-cppgo-built-shim.md),
   and it is what makes "everything is supported" a statement about a mechanism
   rather than a slogan.

3. **Your build is untouched.** The library is built by your build, with your
   flags, and cppgo links its shim against it from the outside. No generated file
   is compiled into your library. For a header-only library there is no library
   at all: the shim is the whole binary.

## The completeness correction

The first accepted design refused constructs the direct ABI could not express:
an inline body, a virtual call, a `std::` type or a by-value non-POD was a build
error. The operator rejected that posture, and
[ADR-0001](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0001-header-first-bindings.md)
records the reason: a binding generator that answers idiomatic C++ with "change
your C++" falls short of the product, because the constructs being refused are
exactly what real headers are made of.

The design that replaced it keeps the fast path for what can take it and pays for
a C++ compiler for everything else. That is why the shim exists, and why the
generator needs a C++ compiler as well as a C++ parser.

## What it buys, and what it costs

Easier:

- you write no binding declarations and no `extern "C"` layer;
- your build gains no step, so a header-only, template-heavy library works;
- the generated package is ordinary Go, with no cgo and no runtime dependency
  beyond `purego`;
- exceptions become Go errors, `std::` types have stated ownership, and integer
  and pointer arguments cost a few nanoseconds per call.

Harder, and stated plainly:

- **generation needs libclang and a C++ compiler**, because the parse and the
  shim build are part of the pipeline;
- **two objects ship** instead of one, and their relative position matters
  ([ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md)
  starts removing that);
- **the generator has to model C++**, and where it does not yet, it refuses
  rather than guesses — see [Limits]({{< ref "explanation/limits.md" >}}). Today
  that refusal list includes enums, inheritance, operators and smart pointers;
- **the shim must match your library's C++ ABI**: same compiler family, same
  standard library, same `_GLIBCXX_USE_CXX11_ABI`. Nothing detects a mismatch
  for you: cppgo reports the compiler it built the shim with, and keeping the
  two in step is a build decision. Mixing them is not a build error; it is
  undefined behaviour at the first call.

## When cppgo is not the answer

- A C library with a stable C API: cgo or `purego` directly is simpler. cppgo's
  value is the C++ side.
- A one-off call into one function: writing one `syscall`/`purego` call is less
  work than installing libclang.
- A library whose API is a macro-only DSL: libclang sees a macro's expansion, not
  its declaration, so cppgo cannot bind it at all.

## Related

- How the pieces fit: [Architecture]({{< ref "explanation/architecture.md" >}}).
- The decisions, with the alternatives they rejected: [Design decisions]({{< ref "explanation/decisions.md" >}}).
