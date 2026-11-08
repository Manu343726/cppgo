---
title: "Architecture"
description: "Three artefacts, two layers, and one call from Go to C++."
weight: 20
---

cppgo is two products that ship together: a **runtime** that Go programs link,
and a **generator** that writes and compiles the C++ side. Everything else in
this page follows from that split and from the three artefacts one generation
produces.

## Two layers

| Layer | Package | Owns | Depends on |
|---|---|---|---|
| runtime | `github.com/Manu343726/cppgo` (repository root) | dynamic loading, symbol resolution, the C ABI trampolines, the reentrant carrier, stack probing, foreign-object storage, wire helpers, error types, the callback table | the standard library, `internal/asm`, `internal/rtl`, `internal/callback`, `purego` |
| generator | `internal/gen`, `internal/cxx`, `cmd/cppgo` | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim, and compiling the shim | anything, including libclang and a C++ compiler |

The runtime **never imports `internal/cxx`**, directly or transitively, and has
no libclang dependency. That is a test, not a convention: a test in the root
package fails when libclang appears in its dependency graph. A program built with
a generated package needs no libclang and no C++ compiler; it needs the two
shared objects at run time.

## Three artefacts

| Artefact | Owner | What it is |
|---|---|---|
| the user's library | you | your sources, your compiler flags, your build. cppgo never writes into it |
| the cppgo shim | cppgo | `zz_cppgo_shim.cpp`, generated from the same parse as the Go API and compiled by cppgo into `libcppgo_<package>.so`, linked against your library |
| the generated Go package | cppgo | `zz_cppgo.gen.go` and `zz_cppgo_amd64.s`: the API you call, one trampoline per binding, and the code that loads the shim |

The dotted line is the contract's edge: **your build is untouched, and no
generated code is in it.**

## One call, step by step

Take `mathx::Vector::at(std::size_t) const`, generated as `At`, carrier
**thunk** because it may throw.

1. **Go calls `At(2)`.** The generated method checks the receiver, calls
   `cppgo.ProbeStack()`, and forms a pointer to its result local.
2. **The trampoline runs.** `cppgoCall_79aeb346bd10` is a NOSPLIT assembly
   function with no Go frame:

   ```asm
   TEXT ·cppgoCall_79aeb346bd10(SB), NOSPLIT, $0-36
   	MOVQ	fn+0(FP), R12
   	MOVQ	this+8(FP), DI
   	MOVQ	a1+16(FP), SI
   	MOVQ	out+24(FP), DX
   	MOVQ	SP, R13
   	ANDQ	$~15, SP
   	CALL	R12
   	MOVQ	R13, SP
   	MOVL	AX, ret+32(FP)
   	RET
   ```

   It reads its arguments before switching the stack pointer, aligns `SP` to 16
   bytes for the System V convention, calls, restores `SP`, and only then stores
   the result. `ProbeStack` ran first because the probe grows the goroutine
   stack, and growing it after taking a pointer into the caller's frame would
   move that frame.

3. **The thunk runs as C++.** The address in `R12` is `cppgo_thunk_79aeb346bd10`
   in the shim, which clears its thread-local message, casts the `this` pointer,
   and calls the real method inside `try`/`catch`.
4. **The result comes back through Go storage.** The thunk writes the `double`
   through the out-pointer the caller provided, and returns `0` or `1`.
5. **The generated method maps the status.** Non-zero becomes a
   `*cppgo.CallError` reading the shim's message; zero returns the value.

A **direct** binding is the same path with step 3 replaced by the C++ symbol
itself and no status: `scale` is `noexcept`, so the call goes straight to
`_ZN5mathx6Vector5scaleEd` and there is nothing to map.

A **reentrant** binding differs at step 2: `cppgo.CallReentrant` fills an
argument block and enters through `runtime.cgocall`, so the thread is inside a
foreign call and a callback can re-enter Go.

## The `std::` ABI

`std::string`, `std::vector` and friends never appear in Go. Their layout exists
only in the shim, which is C++ and can legally construct them:

- a `std::string` parameter is copied into a `std::string` in the shim, and a
  result is copied out into a Go string;
- a `std::vector<T>` is copied element by element in both directions;
- `std::string_view` and `std::span` are borrowed as `(pointer, length)` and are
  valid only for the call;
- the shim and your library must use the same standard library and the same
  `_GLIBCXX_USE_CXX11_ABI`. The generator records the compiler it used and
  rejects a mismatch rather than hoping.

## Building the shim

The shim is one C++ translation unit compiled by cppgo, not by your build. The
command is assembled from the flags discovered for the header
(`compile_commands.json` or `.clangd`), the compiler named by `-cxx` or found as
`g++`/`clang++`, `-fPIC -shared`, and your library as a link input:

- the library is passed as `-L<dir> -l:<file>`, so the linker records its
  **soname** (or its file name) rather than a build path;
- the shim is linked with `-Wl,-rpath,$ORIGIN`, so the library is found beside
  the shim at run time.

A shim that does not compile is a generation error carrying the compiler's
diagnostic and the binding whose thunk it is in, never a symbol that fails at the
first call.

## Loading

The generated package loads **your library first and the shim second**. A shim
symbol is therefore always resolvable; a missing symbol is a load error naming
the shim and the advice to regenerate. Your library's path is resolved in four
steps — `CPPGO_LIBRARY_PATH`, `CppgoLibraryPath`, the shim's own directory, then
the recorded name on the loader's search path — described in
[Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).

## Callbacks

A Go function handed to C++ is a **slot in a table of five-byte `CALL`
instructions**. The instruction a C caller lands on names the slot; the
trampoline saves the fourteen argument registers and calls
`runtime.cgocallback` through a copy of the runtime's host-ABI entry. The
runtime then runs the Go function on the goroutine that made the call, locked to
its M, with the C frames left where they are.

Three consequences are structural:

- the call that may invoke the callback must be made through the **reentrant**
  carrier; on the direct trampoline the same callback kills the process;
- a callback carries at most six INTEGER and eight SSE arguments and one
  register result, because the trampoline reads the register file only;
- a panic inside the callback is captured, the result is zeroed, and the binding
  re-panics in Go when the foreign call returns. It never unwinds into C++.

## Determinism and evidence

The same inputs produce byte-identical Go and C++. Bindings are sorted by
qualified name, and a thunk's name is a stable hash of the qualified signature.
`cppgo check` is the CI gate, and the golden files under `internal/gen/testdata`
are its unit-level equivalent. Every binding carries one evidence line naming
the C++ declaration, its mangled symbol and the reason for its carrier.

## Where the code lives

| Path | Contents |
|---|---|
| [`cppgo.go`](https://github.com/Manu343726/cppgo/blob/main/cppgo.go) | `Open`, `Library`, `LastError` |
| [`loadfs.go`](https://github.com/Manu343726/cppgo/blob/main/loadfs.go) | `OpenFS`, the extraction cache |
| [`object.go`](https://github.com/Manu343726/cppgo/blob/main/object.go) | pinned foreign storage |
| [`reentrant.go`](https://github.com/Manu343726/cppgo/blob/main/reentrant.go) | `CallReentrant`, the callback entry points |
| [`internal/asm`](https://github.com/Manu343726/cppgo/blob/main/internal/asm) | the NOSPLIT call and the stack probe |
| [`internal/callback`](https://github.com/Manu343726/cppgo/blob/main/internal/callback) | the callback table and the C→Go entry |
| [`internal/cxx`](https://github.com/Manu343726/cppgo/blob/main/internal/cxx) | libclang through `purego` |
| [`internal/gen`](https://github.com/Manu343726/cppgo/blob/main/internal/gen) | selection, carriers, the emitters, the shim build |
| [`cmd/cppgo`](https://github.com/Manu343726/cppgo/blob/main/cmd/cppgo) | the command line |
| [`internal/shim`](https://github.com/Manu343726/cppgo/blob/main/internal/shim) | the construct matrix, measured against a real compiler |
| [`internal/errormap`](https://github.com/Manu343726/cppgo/blob/main/internal/errormap) | the error projection the generator does not consume yet |

## Related

- Why the split exists: [Why cppgo exists]({{< ref "explanation/why.md" >}}).
- What the mechanisms cost and where they stop: [Limits]({{< ref "explanation/limits.md" >}}).
- The decisions and their alternatives: [Design decisions]({{< ref "explanation/decisions.md" >}}).
