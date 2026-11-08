+++
title = "Installation"
description = "Go 1.25+, Linux/amd64, libclang for the generator and a C++ compiler because cppgo compiles the generated shim. What to install, how cppgo finds libclang, and how to verify the toolchain."
weight = 10
+++

**Audience:** a machine that has to run `cppgo gen` and build the result.

## Requirements

| requirement | why |
|---|---|
| **Go 1.25 or newer** | the module pins `go 1.25.0`; CI tests stable and `1.25.x` |
| **Linux/amd64** | the generated trampolines are amd64 assembly for the System V ABI. The package still builds for other targets and panics with a clear message |
| **libclang** | `cppgo gen` parses your C++ header with it. Install your distribution's libclang development package (`libclang-dev` is the name the tool's own error message suggests) |
| **a C++ compiler** | cppgo compiles the generated shim with it — `g++`, then `clang++`, or whatever `-cxx` names. It must match the library's compiler family and standard library (see below) |
| **your library's build** | the `.so` you already produce. cppgo never compiles your library, never writes into it, and never asks it to include anything |

The Go side needs no C toolchain and no `import "C"`. The compiler is a
*generation-time* requirement, because the shim is C++ that cppgo owns.

## Install the generator

```console
$ go install github.com/Manu343726/cppgo/cmd/cppgo@latest
```

From a checkout of the repository, `make build` produces the same binary at
`bin/cppgo` (it builds every command with `CGO_ENABLED=0`).

Released binaries are published on the
[releases page](https://github.com/Manu343726/cppgo/releases) for
`linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64` — those are the
targets the release workflow cross-compiles the CLI for. The runtime is still
Linux/amd64 only in this version, so those other binaries can run the generator
but cannot execute a binding.

Verify the command is on your `PATH`:

```console
$ cppgo version
cppgo dev
```

A locally built or `go install`ed binary reports `dev` unless it was built with
`-ldflags "-X main.version=v1.2.3"`.

## Let cppgo find libclang

`internal/cxx` loads libclang at run time through purego — no cgo, no headers
needed. `CPPGO_LIBCLANG` names a specific library; otherwise it searches the
bare names `libclang.so` and `libclang.dylib`, the common system directories,
and the versioned and per-LLVM layouts including the Homebrew paths on macOS.

If it cannot find one, the run fails with an error wrapping
`cxx: libclang not found` and lists the paths it tried:

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx -out ./mathx
cppgo gen: cppgo: include/mathx/vector.hpp: cxx: libclang not found (searched
libclang.so, /usr/lib/libclang.so, ...; install libclang-dev or set CPPGO_LIBCLANG)
```

The search runs once per process; changing the variable takes effect in a new
process.

## The compiler must match the library

The shim calls into your library across the C++ ABI, so both sides must agree on
the compiler family, the standard library and `_GLIBCXX_USE_CXX11_ABI`. cppgo
compiles the shim with the flags it discovered for the header
(`compile_commands.json`, `.clangd`) and with the compiler `-cxx` names (default
`g++`, then `clang++`). It records the compiler in the generated file and
rejects a shim that mixes `libstdc++` with a `libc++` library.

If your library is built with clang and libc++, say so:

```console
$ cppgo gen -header include/af/array.h -package arrayfire \
      -library build/libaf.so -cxx clang++ -out ./arrayfire
```

## Build the C++ library you are binding

cppgo does not compile C++. Build a normal shared object with your own build
system, and give it a **soname**, because that is how the generated shim names
it:

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Wl,-soname,libmathx.so \
      -Iinclude -o build/libmathx.so src/vector.cpp
```

The soname matters for deployment: the shim records `libmathx.so` in its
`DT_NEEDED` entry, never the build path, so the same generated package works
after the tree moves.

## Generate and verify

A project with a compilation database gives the parser the flags the build
uses, which is the difference between validating against the real header and
validating against a guess:

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -compile-commands build/compile_commands.json \
      -out ./mathx
mathx::Vector::Vector(double, double)   thunk  cppgo_thunk_7d8c482bce3e   not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept   direct _ZN5mathx6Vector5scaleEd    noexcept, out of line, not virtual, register class
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

That is the real report: one line per binding, the carrier, the symbol, and the
reason. If the flags were guessed because there was no `compile_commands.json`
or `.clangd`, a warning goes to stderr and the parse may have missed include
paths; [validation]({{< ref "manual/validation.md" >}}) explains how to fix it.

## Deploy the two objects together

`cppgo gen` writes the Go package and compiles the shim beside it. At run time
the shim loads your library through `DT_NEEDED`, and both are linked with
`-Wl,-rpath,$ORIGIN`, so keeping them in one directory is the deployment that
needs no environment:

```console
$ cp build/libmathx.so ./mathx/
$ go run .
```

`CPPGO_LIBRARY_PATH` or the generated `CppgoLibraryPath` points the package at
a library kept somewhere else. For a single file to ship, the runtime provides
`cppgo.OpenFS` for `//go:embed`; wiring it into a generated package is still
open ([#20](https://git.manu343726.com/manu343726/cppgo/issues/20)).

Next: [quickstart]({{< ref "manual/quickstart.md" >}}).
