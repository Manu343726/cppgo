+++
title = "Limitations and roadmap"
description = "What the direct ABI mode cannot do today — arm64, Windows, by-value struct returns, callbacks, templates — and which parts of the design are deliberately not implemented."
weight = 30
+++

cppgo is early, and the honest version of its scope is more useful than a
feature list. Everything here is a current limitation.

## Platform

| | status |
|---|---|
| **Linux/amd64** | supported; the only target that can execute a binding |
| arm64 | not supported. The package *builds*; a call panics with a clear message. arm64 is next on the roadmap |
| Windows | not supported (no `dlopen`/SysV in this form) |
| macOS | not supported at runtime. The CLI cross-builds and libclang discovery includes Homebrew paths, but the trampolines are amd64 SysV; treat any binding as unsupported |
| Go | 1.25 or newer (the module pins `go 1.25.0`) |

## The direct ABI mode

| C++ construct | status | alternative |
|---|---|---|
| free function | yes | |
| `static` member function | yes | |
| non-virtual member, defined out of line | yes | |
| `const` member function | yes | |
| constructor / destructor, out of line | yes | |
| pointer / reference parameters | yes | |
| scalar parameters, `bool`, enums | yes | |
| `string` and `[]T` as pointer + length | yes | |
| struct/class passed by value | **rejected** | take it by reference, or shim mode |
| struct/class returned by value | **rejected in v1** | shim mode |
| virtual function | **rejected** | shim mode |
| inline / header-only function | **rejected** | move it out of line, or shim |
| template never instantiated | **rejected** (no mangled name) | explicit instantiation, or shim |
| `std::string`, `std::vector`, any non-POD by value | **rejected** | change the signature, or shim |
| exceptions crossing the boundary | **undefined behaviour** | catch inside the function or inside your shim entry point |
| callbacks from C++ into Go | **not supported** | batch, or poll from Go |
| class handle as a parameter | **rejected** | `cppgo.Handle` with a pointer, or shim |
| returning a `string` or a slice | **rejected** | write into a caller-owned buffer |
| variadic functions | **rejected** | pass a slice |
| more than 6 integer / 8 XMM arguments | **rejected** | group into a pointer |
| more than one library per Go package | **not supported** | one package per library |

The rejections are deliberate. Each one is a shape where the generated code
would be wrong at run time, and the generator prefers a build error. There is no
mode to opt into. The carrier is chosen from the declaration, and the refusal is
final until the construct is implemented ([validation]({{< ref
"manual/validation.md" >}})).

## Runtime trade-offs

- **The `P` stays with the goroutine during a call.** Short calls are fast. A blocking
  call delays a GC cycle by up to its duration. For long or blocking work, move
  the C++ to a separate process.
- **The foreign frame lives on the goroutine stack**, inside a 32 KiB reserve.
  Deep recursion or a large C++ stack frame can exhaust it.
- **No finalizers.** Every constructor needs an explicit destructor.
- **`Close` leaves in-flight calls running.** The caller owns the ordering.
- **A binary that loads a `.so` is dynamically linked.** The promise covers the
  build, with no cgo and no C toolchain.
- **One `CppgoLibraryPath` per package.** It overrides the single library the
  package declared. There is no registry of libraries.

## The ABI-inference ladder. Two of three tiers wait for an implementation

The design describes three ways to learn a binding's ABI, most-specific first.
**Only tier 1 is implemented.**

| tier | source | status |
|---|---|---|
| 1 | clangd compilation config (`compile_commands.json`, `.clangd`) through libclang | **implemented**; the only tier that can validate and fail the build |
| 2 | infer the ABI from the loaded binary: read `.dynsym`, demangle with `ianlancetaylor/demangle` | **designed but not implemented** |
| 3 | write the ABI down by hand with a `//cppgo:abi` directive | **designed but not implemented** |

This matters in practice:

- The report values `abi from libfoo.so (unvalidated)` and
  `abi from //cppgo:abi directive` that appear in the design document **do not
  exist in this version**. Today the report says `abi from
  compile_commands.json`, `abi from .clangd`, `abi from arguments` or
  `abi from fallback flags (assumed)`.
- The parser rejects `//cppgo:abi`. A package that uses it
  fails to generate.
- There is no `.dynsym` reading and no demangling fallback. If libclang cannot
  see the declaration, the binding cannot be emitted. Use shim mode.

The `internal/cxx` package does read the ELF dynamic symbol table in its own
tests, to cross-check that what the model calls "has an out-of-line body" is what
`nm -D` shows. That is a test oracle.

## Roadmap

In the order the design records them, with no dates:

1. **arm64.** The package must keep building and panicking clearly everywhere
   else until then.
2. **By-value struct and class returns** — the sret slot is already reserved in
   the layout. What is missing is the destination and the Go-side layout of the
   returned type.
3. **Callbacks from C++ into Go.** v1 says batch instead.
4. **Tiers 2 and 3** of the ABI ladder, for libraries shipped without headers.

None of these are promised by this documentation. They are the directions the
design contract names. If a page or a comment here reads as a feature, and the code has not done it, the code is right and the page is wrong, please treat it
that way.

## What works today

- **No cgo at build time.** `CGO_ENABLED=0` builds and tests the library.
- **No C toolchain to build the Go side.** You need one for *your* C++ library,
  not for cppgo.
- **No `import "C"` anywhere.**
- **Symbols are resolved at run time.** The same binary can load a different
  build of the library.

Next is [FAQ]({{< ref "project/faq.md" >}}).
