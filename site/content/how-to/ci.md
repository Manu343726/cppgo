---
title: "Gate a generated package in CI"
description: "cppgo check, the two test passes, and the toolchain you must pin."
weight: 60
---

A generated package is one more artefact whose sources are in the repository, so
a pipeline has two questions to answer: *are the committed sources current?* and
*does the package still work?* `cppgo` answers the first with `cppgo check` and
the second with the same two test passes the project runs on itself.

## 1. Pin the toolchain

The generator's output depends on the compiler's diagnostics and on libclang's
model of the header, so pin the toolchain the same way you pin a compiler for
any other build step.

| Component | Constraint | Why |
|---|---|---|
| libclang | **20 or newer** | `clang_getOffsetOfBase`, used to read base-subobject offsets, is not exported before 20; a tree built against 19 fails at load time |
| C++ compiler | C++17, same family and standard library as your library | the shim calls your library across the C++ ABI |
| Go | 1.25 or newer | `go.mod` declares `go 1.25.0` |
| `cppgo` | a pinned version | the generated output is reproducible per version; a different generator may write different code |

The project's own image is the evidence for the first two rows: it pins
`ARG CLANG_VERSION=22` and installs `g++` from the base distribution
([Dockerfile](https://github.com/Manu343726/cppgo/blob/main/Dockerfile)).
Using that image, or your own with the same pins, is the least surprising option.

## 2. Build your library first

The shim links against your library and its `.dynsym` is what proves a direct
binding, so the library must exist before generation. This is your build's step,
not cppgo's:

```console
$ cmake --build build --target mylib      # or: make lib
```

## 3. Commit the generated sources and check them

The Go, the assembly and the shim sources are deterministic for the same inputs,
so commit them and fail the pipeline when they drift:

```console
$ cppgo gen   -header include/mylib/api.hpp -package mylib \
              -library build/libmylib.so -out ./mylib
$ cppgo check -header include/mylib/api.hpp -package mylib \
              -library build/libmylib.so -out ./mylib
./mylib: generated files are up to date
```

`cppgo check` regenerates in memory, compares with the files on disk, writes
nothing and compiles nothing. Its exit codes are part of its interface:

| Exit | Meaning |
|---|---|
| `0` | every generated file matches |
| `1` | a file is missing or stale — run `cppgo gen` and commit the result |
| `2` | the command line is wrong: a missing `-header`, an unknown flag, an unexpected argument |

A stale file is named:

```
cppgo check: ./mylib is out of date (run "cppgo gen"): zz_cppgo_shim.cpp
```

For a template-heavy package the shim depends on the program's instantiations, so
check has to scan the same program the build scans:

```console
$ cppgo check -header include/box.hpp -package box -out ./box -scan ./cmd/mytool
```

## 4. Test in two passes

The generated package and the runtime must build and run with cgo disabled. The
race detector, however, is built on cgo, and the toolchain rejects `-race` with
cgo off:

```console
$ CGO_ENABLED=0 go test -count=1 ./...
$ CGO_ENABLED=1 go test -race -count=1 ./...
```

That is why the project's `make test` runs two passes rather than one command
([`Makefile`](https://github.com/Manu343726/cppgo/blob/main/Makefile)).
Run both: the first proves the no-cgo property, the second finds data races in
your package and in the glue around it.

Your tests need the two objects at run time, exactly like a deployment. The
examples pass their paths through the environment and set `CppgoLibraryPath`
before the first call
([`examples/mathx/mathx_test.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/mathx_test.go)):

```go
func init() {
	path := os.Getenv("CPPGO_MATHX_LIB")
	if path == "" {
		path = filepath.Join("build", "libmathx.so")
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}
```

## A pipeline skeleton

```yaml
# The five steps, whatever the CI system is:
# 1. install or pull the pinned image (libclang >= 20, a C++17 compiler, Go)
# 2. build the C++ library with the project's own build
# 3. cppgo gen   (to produce) / cppgo check (to verify a clean tree)
# 4. CGO_ENABLED=0 go test -count=1 ./...
# 5. CGO_ENABLED=1 go test -race -count=1 ./...
```

Nothing else is required. There is no cache to warm: generation reads the
headers and the compilation configuration and writes files, and its determinism
means an unchanged tree produces no diff.

## What can go wrong, and what it looks like

| Symptom | Cause |
|---|---|
| `cxx: libclang not found` | no libclang on the machine; the generator needs it, the program does not |
| `undefined symbol: clang_getOffsetOfBase` | libclang older than 20 |
| `no C++ compiler found: the shim is C++ and cppgo compiles it itself` | neither `g++` nor `clang++` is on `PATH`, and no `-cxx` was given |
| `the generated shim does not compile; …` | the shim was compiled with flags that do not match the header, or a construct the build does not emit was accepted by the parse |
| the check names one file | regenerate and commit; a hand edit to a generated file is also caught here |

## Related

- The command reference: [Command line]({{< ref "reference/cli.md" >}}).
- What to pin, and why the generator needs a C++ parser: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- The two artefacts a deployment needs: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
