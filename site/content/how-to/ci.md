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

A *pipeline* is a file in the repository that a hosted machine — the *runner* —
executes step by step. Each step is a shell command, and the pipeline fails at the
first step that exits non-zero, so the order of the steps decides what a reader
learns from a red run. *cgo* is Go's built-in way to call C: the part of the
toolchain that, when enabled, links a C compiler into a Go build. cppgo exists so
that a generated binding needs no cgo at all, and a step below proves it.

Use this guide when your repository commits the generated sources and runs its
tests in CI. The scenario is the `full` example: its `make check` and `make test`
targets are the two steps below.

## 1. Pin the toolchain

Pin the tools that generate, the same way you pin a compiler for any other build
step:

| Component | Constraint | Why |
|---|---|---|
| libclang | **20 or newer** | libclang is the C library of the Clang compiler front end, and `cppgo gen` uses it to parse each header; older releases lack `clang_getOffsetOfBase`, and generation fails at load time |
| C++ compiler | C++17, the same family and standard library as your library | cppgo compiles the shim — the C++ side it generates — with it, and the shim calls your library across the ABI, the way a compiled call passes its arguments and result between two separately built binaries |
| Go | 1.25 or newer | [`go.mod`](https://github.com/Manu343726/cppgo/blob/main/go.mod) declares `go 1.25.0` |
| `cppgo` | one fixed version | a different generator version may write different bytes into the committed sources |

Use a container image or a runner with these versions pinned rather than taking
whatever the runner happens to have. That is the least surprising option, and the
only one that keeps generation reproducible across machines: the check in step 3
compares bytes, so a generator or libclang that formats a declaration differently
turns a correct tree red.

Install the generator at one fixed version — `go install
github.com/Manu343726/cppgo/cmd/cppgo@<version>` for a tagged release, or a build
from the commit you pin — and print it in the log so a later reader can tell
which one produced the run. `cppgo version` writes `cppgo <version>`, and `dev`
for a binary built from a working tree.

## 2. Build your library first

Generation reads what your library exports and records the name it is loaded by,
so the library has to exist before `cppgo gen` runs. This is your build's step,
not cppgo's:

```console
$ cmake --build build --target mylib      # or: make lib
```

Skip it and the generator cannot see the exported symbols, so it cannot prove
that a call is carried by the *direct path* (the Go side calls the C++ function
itself, with no shim in between), and it has no *soname* — the short name the
loader resolves the library by — for the shim to depend on. A `-library` path
that does not exist is not an error at generation time: the file is simply not
read. The package it writes still compiles; the shim it builds then fails the
moment the program opens it, because nothing was linked in to provide the
functions it calls, and the loader names the first one missing:

```
cppgo: open the shim gen/libcppgo_demo.so: cppgo: open "gen/libcppgo_demo.so": rtl: dlopen in "gen/libcppgo_demo.so": gen/libcppgo_demo.so: undefined symbol: _ZN4demo3addEii
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
scans, or it will find the specializations missing — the concrete template types
your program uses, which the scan collects. The example's `make check` above
already does it, because its target passes `-scan ./cmd/... -scan-dir .`.
The page on the two phases is
[Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## 4. Test in two passes

The generated package and the runtime must build and run with cgo disabled. The
race detector — `-race`, which instruments memory accesses and reports the ones
that are not synchronized — needs cgo, because the runtime's race support is
compiled from C, and the toolchain rejects `-race` with cgo off:

```console
$ CGO_ENABLED=0 go test -count=1 ./...
$ CGO_ENABLED=1 go test -race -count=1 ./...
```

`CGO_ENABLED` is the environment variable the Go toolchain reads: `0` removes cgo
from the build entirely, `1` enables it. `-count=1` disables the Go test cache, so
the step runs the tests instead of replaying a green result from an earlier run.

Run both: the first proves the no-cgo property, the second finds data races in
your package and in the glue around it. Neither pass loads the C++ files on its
own — the tests have to name them, exactly like a deployment.

Your tests need the two C++ files at run time, and `go test` runs each package
with its own directory as the working directory, so a relative path from the
repository root is wrong. The example's test handles both facts in `TestMain`:

```go
// testPath names one of the two objects relative to the example root. A test
// runs with its package's directory as the working directory, so the root is
// two levels up, whichever directory `go test` was started from.
func testPath(rel string) string {
	path := filepath.Join("..", "..", rel)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func TestMain(m *testing.M) {
	metrics.CppgoLibraryPath = testPath(filepath.Join("build", "libmetrics.so"))
	shim := os.Getenv("CPPGO_FULL_SHIM")
	if shim == "" {
		shim = testPath(filepath.Join("gen", "libcppgo_metrics.so"))
	}
	if err := metrics.Open(shim); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in the example root first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
```

`TestMain` runs once before any test in the package. It sets
`CppgoLibraryPath` — the package variable the generated code reads when it opens
the library — to an absolute path, and calls `Open` on the shim. The variable is
read once, at open time, so it has to be set before the first generated call;
`TestMain` is the earliest point a test package controls.

## A pipeline skeleton

```text
# The five steps, whatever the CI system is:
# 1. install or pull the pinned image (libclang >= 20, a C++17 compiler, Go)
# 2. build the C++ library with the project's own build
# 3. cppgo gen (to produce) / cppgo check (to verify a clean tree)
# 4. CGO_ENABLED=0 go test -count=1 ./...
# 5. CGO_ENABLED=1 go test -race -count=1 ./...
```

Nothing else is required. No step depends on a warm cache: `-count=1` makes the
test result independent of the Go test cache, and an unchanged tree produces no
diff for step 3.

## What can go wrong, and what it looks like

| Symptom | Cause |
|---|---|
| `cxx: libclang not found` | no libclang on the machine; the generator needs it, the program does not. Install `libclang-dev`, or point `CPPGO_LIBCLANG` at the shared library file — when set, it is the only candidate considered |
| `undefined symbol: clang_getOffsetOfBase` | libclang older than 20 |
| `no C++ compiler found: the shim is C++ and cppgo compiles it itself` | neither `g++` nor `clang++` is on `PATH`, and no `-cxx` was given |
| `cppgo: the generated shim does not compile: …` | the flags cppgo used do not match the header, or a construct this build does not support was accepted by the parse |
| `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` | the race pass ran with cgo disabled; set `CGO_ENABLED=1` for that step |
| the check names one file | regenerate and commit; a hand edit to a generated file is also caught here |

## Related

- The command reference: [Command line]({{< ref "reference/cli.md" >}}).
- How to supply the compilation configuration: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- The two files a deployment needs: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
