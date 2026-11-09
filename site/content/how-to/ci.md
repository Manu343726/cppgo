---
title: "Gate a generated package in CI"
description: "cppgo check, the two test passes, and the toolchain you must pin."
weight: 60
---

A generated package is one more artefact whose sources are in the repository, so
a pipeline has two questions to answer: *are the committed sources current?* and
*does the package still work?* `cppgo` answers the first with `cppgo check` and
the second with two test passes, one with cgo disabled and one with the race
detector.

Use this guide when your repository commits the generated sources and runs its
tests in CI. The scenario is the `full` example: its `make check` and `make test`
targets are the two steps below.

## 1. Pin the toolchain

Pin the tools that generate, the same way you pin a compiler for any other build
step:

| Component | Constraint | Why |
|---|---|---|
| libclang | **20 or newer** | older releases lack a function the generator uses, and generation fails at load time |
| C++ compiler | C++17, the same family and standard library as your library | cppgo builds the C++ side with it |
| Go | 1.25 or newer | `go.mod` declares `go 1.25.0` |
| `cppgo` | a pinned version | a different generator version may write different sources |

Use an image or a runner with these versions pinned rather than taking whatever
the runner happens to have. That is the least surprising option, and the only one
that keeps generation reproducible across machines.

## 2. Build your library first

Generation reads what your library exports, so the library has to exist before
`cppgo gen` runs. This is your build's step, not cppgo's:

```console
$ cmake --build build --target mylib      # or: make lib
```

## 3. Commit the generated sources and check them

The generated sources are reproducible for the same inputs, so commit them and
fail the pipeline when they drift. The example's check target is a single
command:

```console
$ make check
build/cppgo check -header include/metrics/metrics.hpp -package metrics -library build/libmetrics.so -compile-commands build/compile_commands.json -out ./gen -status metrics::validate_name -scan ./cmd/... -scan-dir .
./gen: generated files are up to date
```

Your own command repeats the flags you pass to `cppgo gen`, with `check` in place
of `gen`:

```console
$ cppgo check -header include/mylib/api.hpp -package mylib \
              -library build/libmylib.so -out ./mylib
./mylib: generated files are up to date
```

`cppgo check` writes nothing and builds nothing. Its exit codes are part of its
interface:

| Exit | Meaning |
|---|---|
| `0` | every generated file matches |
| `1` | a file is missing or stale — run `cppgo gen` and commit the result |
| `2` | the command line is wrong: a missing `-header`, an unknown flag, an unexpected argument |

A stale file is named:

```
cppgo check: ./mylib is out of date (run "cppgo gen"): zz_cppgo_shim.cpp
```

For a template-heavy package, check has to scan the same program the build
scans, or it will find the specializations missing. The example's `make check`
above already does it, because its target passes `-scan ./cmd/... -scan-dir .`.
The page on the two phases is
[Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## 4. Test in two passes

The generated package and the runtime must build and run with cgo disabled. The
race detector needs cgo, and the toolchain rejects `-race` with cgo off:

```console
$ CGO_ENABLED=0 go test -count=1 ./...
$ CGO_ENABLED=1 go test -race -count=1 ./...
```

Run both: the first proves the no-cgo property, the second finds data races in
your package and in the glue around it.

Your tests need the two C++ files at run time, exactly like a deployment. Set the
path before the first call:

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

```text
# The five steps, whatever the CI system is:
# 1. install or pull the pinned image (libclang >= 20, a C++17 compiler, Go)
# 2. build the C++ library with the project's own build
# 3. cppgo gen (to produce) / cppgo check (to verify a clean tree)
# 4. CGO_ENABLED=0 go test -count=1 ./...
# 5. CGO_ENABLED=1 go test -race -count=1 ./...
```

Nothing else is required. There is no cache to warm: an unchanged tree produces
no diff.

## What can go wrong, and what it looks like

| Symptom | Cause |
|---|---|
| `cxx: libclang not found` | no libclang on the machine; the generator needs it, the program does not |
| `undefined symbol: clang_getOffsetOfBase` | libclang older than 20 |
| `no C++ compiler found: the shim is C++ and cppgo compiles it itself` | neither `g++` nor `clang++` is on `PATH`, and no `-cxx` was given |
| `the generated shim does not compile; …` | the flags cppgo used do not match the header, or a construct this build does not support was accepted by the parse |
| the check names one file | regenerate and commit; a hand edit to a generated file is also caught here |

## Related

- The command reference: [Command line]({{< ref "reference/cli.md" >}}).
- How to supply the compilation configuration: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- The two files a deployment needs: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
