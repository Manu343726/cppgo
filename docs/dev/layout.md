# Layout

Where the code lives and how the two layers are kept apart. This is developer
material; the manual links to [Architecture](../../site/content/explanation/architecture.md)
for the user-facing description of how a call works.

## Two layers

| Layer | Package | Owns | Depends on |
|---|---|---|---|
| runtime | `github.com/Manu343726/cppgo` (repository root) | dynamic loading, symbol resolution, the C ABI trampolines, the reentrant carrier, stack probing, foreign-object storage, wire helpers, error types, the callback table | the standard library, `internal/asm`, `internal/rtl`, `internal/callback`, `purego` |
| generator | `internal/gen`, `internal/cxx`, `cmd/cppgo` | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim, and compiling the shim | anything, including libclang and a C++ compiler |

The runtime **never imports `internal/cxx`**, directly or transitively, and has
no libclang dependency. `TestRuntimeHasNoGeneratorDependency` (root package) runs
`go list -deps` and fails when `internal/cxx`, `cmd/cppgo` or `internal/gen`
appears in the runtime's dependency graph. A program built with a generated
package needs no libclang and no C++ compiler; it needs the two shared objects at
run time.

## Repository

| Path | Contents |
|---|---|
| `cppgo.go`, `loadfs.go`, `object.go`, `reentrant.go`, `errors.go`, `callerror.go`, `stack.go`, `wire.go`, `handle.go` | the runtime package the generated code imports: `Open`, `OpenFS`, foreign storage, the reentrant carrier, the error types |
| `internal/asm` | the NOSPLIT call trampolines and the stack probe |
| `internal/rtl` | the loader and the C string helpers under `purego` |
| `internal/callback` | C++→Go callbacks through `runtime.cgocallback` |
| `internal/cxx` | libclang, reached through `purego` |
| `internal/gen` | selection, carrier routing, the Go/asm/shim emitters, the shim compile |
| `internal/errormap` | the error projection and its configuration (not consumed by the generator yet) |
| `internal/shim` | the construct matrix, measured against a real compiler |
| `internal/testlib` | test scaffolding that builds the C++ fixtures |
| `cmd/cppgo` | the command line |
| `examples/` | `mathx` and `stats`, each with its own build and tests |
| `site/` | the Hugo sources of the documentation site |
| `docs/adr/` | the decision records |
| `docs/dev/` | this folder |

## The generated files

`cppgo gen` writes, under `-out`:

| File | Contents |
|---|---|
| `zz_cppgo.gen.go` | the Go API, a load function, and one declaration per trampoline |
| `zz_cppgo_amd64.s` | one NOSPLIT assembly trampoline per binding: it aligns the stack, makes the call, restores it |
| `cppgo_shim/zz_cppgo_shim.h` | the `extern "C"` prototypes of the thunks |
| `cppgo_shim/zz_cppgo_shim.cpp` | the thunks, one per non-direct binding |
| `libcppgo_<package>.so` | the shim, compiled by cppgo and linked against the user's library |

The first four are sources; the `.so` is a build artefact. A header-only library
has no shared object of its own: the shim is the whole binary.
