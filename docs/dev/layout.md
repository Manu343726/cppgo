# Layout

Where the code lives, what each package owns, and why the runtime and the
generator are kept apart. cppgo reads a C++ header and writes a Go package that
calls the library at run time without cgo; this page is the map of the code that
does it. Read it when you need to know where a change belongs: a C++ declaration
that is not bound, a call that crashes, a generated file you are tempted to edit,
or a command-line flag.

It is developer material. The user-facing description of how a call works is
[Architecture](../../site/content/explanation/architecture.md), and the decision
that produced the two layers is [ADR-0002](../adr/0002-cppgo-built-shim.md).

## The words this page uses

| Term | What it means here |
|---|---|
| **FFI** | foreign function interface: Go calling code that was not compiled as Go, through the C calling convention — the platform's rule for which registers and which memory carry an argument and its result |
| **runtime** | the Go package a generated package imports: [`github.com/Manu343726/cppgo`](../../cppgo.go), the repository root. It loads shared objects, resolves symbols and makes the call; it does not parse C++ |
| **generator** | the code that reads a C++ header and writes the Go package: [`internal/cxx`](../../internal/cxx) parses, [`internal/gen`](../../internal/gen) emits, [`cmd/cppgo`](../../cmd/cppgo) is the command |
| **generated package** | the Go package `cppgo gen` writes for one C++ library; it contains `zz_cppgo.gen.go`, `zz_cppgo_amd64.s` and the shim sources |
| **trampoline** | generated assembly that performs one call into C++: it loads the arguments into the C argument registers, aligns the stack and calls the address |
| **thunk** | generated C++ that adapts a declaration the trampoline cannot carry — an inline body, a virtual call, a `std::` type, a by-value record — into a plain `extern "C"` function |
| **shim** | the C++ library cppgo generates and compiles from the thunks, `libcppgo_<package>.so`; the generated Go package loads it instead of calling the user's library directly where a thunk is needed |
| **carrier** | the mechanism that carries one binding across the boundary: the direct trampoline, a thunk in the shim, or the reentrant call that can enter Go again for a callback |
| **shared object, `soname`** | a compiled library file (`.so` on Linux) the loader opens at run time, and the name recorded inside it that the loader resolves when another object depends on it |
| **cgo** | Go's built-in way to call C: it needs `CGO_ENABLED=1` and a C compiler at build time. cppgo's runtime and generated packages are built with `CGO_ENABLED=0` |
| **`NOSPLIT`** | an assembler flag on a Go function that forbids the stack-growth preamble. A trampoline carries it so the call cannot trigger a stack copy in the middle of the foreign call |
| **ABI** | application binary interface: which registers and which memory carry the arguments and the result of a compiled call |
| **libclang** | the C library of the Clang compiler front end; it parses a header into the declarations the generator emits from |
| **`extern "C"`** | a C++ declaration with C linkage: the C calling convention and a name that is not mangled |
| **mangled name** | the name a C++ compiler writes into the object for a symbol, encoding its namespace and parameter types: `_ZN5mathx6Vector4normEv` is `mathx::Vector::norm()` |
| **`std::`** | the namespace of the C++ standard library (`std::string`, `std::vector`, …) |
| **`dlopen`, `dlsym`, `dlclose`** | the C library calls that open a shared object, look a symbol up in it and close it |
| **`ld.so`** | the Linux dynamic loader: the program that opens a shared object and resolves its symbols when a process starts |
| **`DT_NEEDED`** | the list, inside a shared object, of the other shared objects it needs at load time |
| **`runtime.cgocallback`** | the Go runtime's entry point for running a Go function that C called; the callback trampoline aims at it |
| **`gofmt`, `go vet`** | Go's source formatter and its static checker. The `unsafeptr` check that `go vet` runs flags a pointer converted through `uintptr` |
| **golden file** | a committed expected output that a test compares byte for byte |

The carrier definitions, with the code of each, are in
[Carriers](../../site/content/explanation/carriers.md).

## Two layers

| Layer | Packages | Owns | Depends on |
|---|---|---|---|
| runtime | the [root package](../../cppgo.go), [`internal/asm`](../../internal/asm), [`internal/rtl`](../../internal/rtl), [`internal/callback`](../../internal/callback), [`internal/elfdeps`](../../internal/elfdeps) | dynamic loading, symbol resolution, the C ABI trampolines, the reentrant carrier, stack probing, foreign-object storage, wire helpers, error types, the callback table | the standard library, [`purego`](https://github.com/ebitengine/purego) |
| generator | [`internal/cxx`](../../internal/cxx), [`internal/gen`](../../internal/gen), [`cmd/cppgo`](../../cmd/cppgo) | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim, and compiling the shim | anything, including libclang and a C++ compiler |

The split is a promise to the program you ship: it must not need libclang or a
C++ compiler, because those are generation-time tools. The runtime therefore
never imports `internal/cxx`, directly or transitively, and has no libclang
dependency. [`TestRuntimeHasNoGeneratorDependency`](../../deps_test.go) runs
`go list -deps .` — the real import graph, transitive imports included — and
fails when `internal/cxx`, `cmd/cppgo` or anything under `internal/gen` appears
in it.

A program built with a generated package needs the two shared objects at run
time: the shim and the user's library.

## The runtime packages

| Package | Owns | Why it is separate |
|---|---|---|
| [root](../../cppgo.go) (`cppgo`) | `Open`, `OpenFS`, `Library` and its symbol cache, `ProbeStack`, `Handle`/`Object` and the other storage types, `Ptr`/`StrPtr`/`KeepAlive`, the error types, the callback entry point | it is the only package a generated program imports, so its surface is the runtime's public contract |
| [`internal/asm`](../../internal/asm) | the direct and reentrant call trampolines, and the stack probe, as Go assembly (the Go toolchain's own assembler syntax) for amd64, with a non-amd64 fallback whose first foreign call panics | Go assembly is the one place that can enter the C ABI without cgo; keeping it behind a Go wrapper lets the rest of the runtime be ordinary Go |
| [`internal/rtl`](../../internal/rtl) | `dlopen`, `dlsym` and `dlclose`, wrapped from `purego`, with errors that name the library and the symbol | it isolates the loader and the one unsafe conversion a symbol address needs, so the `unsafeptr` check has one place to allow |
| [`internal/callback`](../../internal/callback) | the C++→Go direction: the callback table, `runtime.cgocallback`'s trampoline, panic capture, the child-process tests for a callback that cannot be carried | the reentrant path is the only one that runs Go code inside a foreign call, and its failure mode is a process-wide `fatal error`; it is kept where it can be tested in a child process |
| [`internal/elfdeps`](../../internal/elfdeps) | the `DT_NEEDED` closure of a shared object and the `ld.so` cache, read in pure Go | the runtime decides what an embedded set contains and in which order to preload it; that needs no cgo and no `ldd` process |

## The generator packages

| Package | Owns | Why it is separate |
|---|---|---|
| [`internal/cxx`](../../internal/cxx) | libclang through `purego`: the declaration model, mangled names, layout, access and template facts, diagnostics, flag discovery from `.clangd` and `compile_commands.json` | it is the only package that talks to libclang, so the parsing can be tested and replaced without touching the emitters |
| [`internal/gen`](../../internal/gen) | binding selection, carrier routing, the Go/assembly/shim emitters, the golden files, the shim compile, and the two-phase template scan | it turns a model into files; keeping it separate from `internal/cxx` is what lets a generated package be tested against a model a header cannot produce |
| [`cmd/cppgo`](../../cmd/cppgo) | the `cppgo` command: `gen`, `check`, `build` and `version`, argument parsing, exit codes | the command owns the flags and the messages a user sees; the packages own the work |

## Test and research packages

These are not part of a shipped program:

| Package | Holds |
|---|---|
| [`internal/shim`](../../internal/shim) | a test-only fixture: [`testdata/user.cpp`](../../internal/shim/testdata/user.cpp) is the user's library, [`testdata/shim.cpp`](../../internal/shim/testdata/shim.cpp) is a shim written the way the generator writes one. The tests compile the two as separate objects and assert which one exports what |
| [`internal/testlib`](../../internal/testlib) | the small C++ shared object the runtime tests call into, built on demand with the system `g++` into a temporary directory |
| [`internal/errormap`](../../internal/errormap) | the error projection and its configuration. It is implemented and tested, but the generator does not consume it yet ([#19](https://git.manu343726.com/manu343726/cppgo/issues/19)) |
| [`internal/stackalloc`](../../internal/stackalloc), [`internal/zerocopy`](../../internal/zerocopy), [`internal/declspec`](../../internal/declspec) | the prototypes behind [Stack allocation](stack-allocation.md). They sit behind the `stackalloc` build tag, so the production build, vet, test and coverage runs never compile them |

## The rest of the repository

| Path | Contents |
|---|---|
| [`examples/`](../../examples) | six executable projects, each with its own build: [`mathx`](../../examples/mathx) (a class), [`stats`](../../examples/stats) (slices and a free function), [`full`](../../examples/full) (CMake, templates, callbacks, errors), and the three target modules [`opencv`](../../examples/opencv), [`arrayfire`](../../examples/arrayfire) and [`eigen`](../../examples/eigen) |
| [`benchmarks/`](../../benchmarks) | the cppgo vs cgo vs plain-Go comparison suite and its committed results |
| [`site/`](../../site) | the Hugo sources of the manual, in four sections: tutorials, how-to, reference and explanation |
| [`docs/adr/`](../adr) | one decision record per decision, never edited after acceptance |
| [`docs/dev/`](README.md) | this folder |
| [`scripts/`](../../scripts) | `build-examples.sh`, `coverage-gate.sh`, `docker-test.sh` |
| [`Dockerfile`](../../Dockerfile) | the pinned CI toolchain image |
| [`.forgejo/workflows/`](../../.forgejo/workflows) | the gate the project's own forge runs |
| [`.github/workflows/`](../../.github/workflows) | the GitHub mirror: the same gate, the image build, Pages, and the tag-triggered release |

## The target modules

[`examples/opencv`](../../examples/opencv), [`examples/arrayfire`](../../examples/arrayfire)
and [`examples/eigen`](../../examples/eigen) bind a slice of a real, installed library
rather than a fixture in the tree. There is no C++ source in those directories: the
library is the system's, so the module's `make test` generates the package from the
installed headers, builds the shim against the library, and asserts values the library
itself defines. On a machine without the library every target of the module prints the
missing path and exits zero, so a run there is a skip and not evidence.

[`scripts/test-targets.sh`](../../scripts/test-targets.sh) runs those three test targets
by name, and the `targets` job of [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)
runs it, with the real-header smoke in
[`internal/gen/targets_smoke_test.go`](../../internal/gen/targets_smoke_test.go) (the tag
`cppgo_targets` selects the file), in the pinned image that carries the three libraries.
The script names a library it cannot find as skipped, and the smoke step counts one
`parsed` line per library, so a skip cannot pass for a measurement.

The slices stop where the generator does, and that boundary is
[#112](https://git.manu343726.com/manu343726/cppgo/issues/112)'s subject:

- **`cv::Mat` and `af::array`** each declare something the generator cannot carry —
  `Mat(int, int, int, void *)`, and `array::set(af_array)` where `af_array` is `void *` —
  and one such declaration refuses the whole class, so neither binds its constructors.
  A by-value record crosses since
  [#102](https://git.manu343726.com/manu343726/cppgo/issues/102), which is why the smoke
  hands Go a `cv::Mat` and an `af::array` through a C++ free function; the modules bind
  neither.
- **Eigen's matrix types** are refused earlier. An explicit specialization is the only
  spelling libclang reports, and `template class Eigen::Matrix<...>;` does not compile:
  an explicit instantiation instantiates every constructor, and Eigen's constructors
  static-assert the vector sizes they are declared for, so no matrix size passes.

## The generated files

`cppgo gen` writes, under the directory named by `-out`:

| File | Contents |
|---|---|
| [`zz_cppgo.gen.go`](../../examples/mathx/zz_cppgo.gen.go) | the Go API, an `Open` function that loads the objects, and one declaration per trampoline |
| [`zz_cppgo_amd64.s`](../../examples/mathx/zz_cppgo_amd64.s) | one `NOSPLIT` assembly trampoline per binding the reentrant carrier does not take — the direct and the thunk carriers — plus `cppgoFree` when the package frees C++ storage; each trampoline aligns the stack, makes the call and restores it. Measured: `grep -c '^TEXT '` is 13 in [`examples/mathx/zz_cppgo_amd64.s`](../../examples/mathx/zz_cppgo_amd64.s) (12 thunks + 1 direct) and 17 in [`examples/full/gen/zz_cppgo_amd64.s`](../../examples/full/gen/zz_cppgo_amd64.s) (16 trampolines + `cppgoFree`) |
| [`cppgo_shim/zz_cppgo_shim.h`](../../examples/mathx/cppgo_shim/zz_cppgo_shim.h) | the `extern "C"` prototypes of the thunks |
| [`cppgo_shim/zz_cppgo_shim.cpp`](../../examples/mathx/cppgo_shim/zz_cppgo_shim.cpp) | the thunks, one per binding the shim carries |
| `libcppgo_<package>.so` | the shim, compiled by cppgo and linked against the user's library. `<package>` is the name given to `-package` |

The first four are sources and are committed by the examples and the benchmark
suite, so `cppgo check` can prove they are current. The `.so` is a build
artefact and is not committed. A header-only library has no shared object of its
own: the shim is the whole binary.

`cppgo build -scan <go-package>` is the second phase of the template flow: it
reads the instantiations the Go program uses, adds the explicit specialisations
and their thunks to the shim sources, and recompiles the shim. The file names do
not change.

Generation writes nothing until the shim has compiled in a scratch directory: a
header that produces C++ cppgo cannot compile leaves the output directory as it
was.
