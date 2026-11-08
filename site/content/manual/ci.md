+++
title = "cppgo check in CI"
description = "Make the generated bindings a checked artifact: what cppgo check compares, its exit codes, a GitHub Actions job that fails on stale bindings, and how to keep libclang and the compiler available on the runner."
weight = 90
+++

**Audience:** you are wiring a repository that commits its generated package.

## What `check` does

`cppgo check` is `cppgo gen` with the writing turned off. It regenerates every
source in memory, compares it byte for byte with what is on disk, and reports the
difference:

```console
$ cppgo check -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
./mathx is out of date (run "cppgo gen"): zz_cppgo.gen.go, zz_cppgo_amd64.s
$ echo $?
1
```

When everything is current it prints one line and exits 0:

```console
$ cppgo check -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
./mathx: generated files are up to date
```

Exit codes are the CLI's usual ones:

| code | meaning |
|---|---|
| `0` | generated files are up to date, or the command succeeded |
| `1` | generation failed, or a file is missing or stale |
| `2` | the command line itself was wrong (unknown flag, missing `-header`, extra argument) |

`-q` suppresses the success message. A CI log wants only the failure. The failure
message still goes to stderr and still exits 1.

## Why check instead of gen in CI

`gen` writes the package and **compiles the shim**. On a CI runner that means a
build that "passes" while producing a diff nobody looks at. `check` is the
read-only form. It proves that the committed Go, assembly and shim sources are
exactly what the current headers and flags produce. If someone edits a header and
forgets to regenerate, the job fails and the error names the stale files.

The comparison covers the sources and leaves the compiled `libcppgo_<package>.so` to `gen`.
That binary is a build artefact. It is verified by compiling it, never by
comparing bytes.

## A GitHub Actions job

The generator needs libclang. A `check` job runs on libclang alone. It writes and builds no artefacts. A job that also runs `cppgo gen`
or builds the package's tests does.

```yaml
name: bindings

on:
  push:
    paths:
      - 'bindings/**'
      - 'cpp/**'
  pull_request:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.x'

      - name: Install libclang (the generator parses the header with it)
        run: |
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends clang libclang-dev
          ls /usr/lib/llvm-*/lib/libclang.so* >/dev/null

      - name: Install the generator
        run: go install github.com/Manu343726/cppgo/cmd/cppgo@latest

      - name: Check that the bindings are current
        run: |
          cppgo check -header include/mathx/vector.hpp -package mathx \
            -library build/libmathx.so -out ./mathx -q
```

If the job also builds or tests the generated package, it needs the C++
compiler and the two objects:

```yaml
      - name: Build the library and the cppgo shim
        run: |
          make -C cpp library
          cppgo gen -header include/mathx/vector.hpp -package mathx \
            -library build/libmathx.so -out ./mathx
          cp build/libmathx.so ./mathx/
```

`cppgo check` and `cppgo gen` take the same flags, so a project should keep one
invocation in its `Makefile` and call it twice with different verbs.

If your project has a `compile_commands.json`, pass it so the parse uses the
same flags as the build. This is the difference between validating against the
real headers and validating against a guess:

```yaml
      - name: Check that the bindings are current
        run: |
          cppgo check -header include/mathx/vector.hpp -package mathx \
            -library build/libmathx.so -compile-commands build \
            -out ./mathx -q
```

Without it, the generator may fall back to `-std=c++17 -I<header dir>` and print
the assumed-flags warning. The warning alone is harmless, but a header whose own includes are missing will parse incompletely, and a construct that should
have been refused might pass, or the reverse.

## Cache and reproducibility

- `cppgo check` is deterministic. It compares bytes. The emitter sorts every
  list and derives thunk names from a stable hash, so a timestamp or a map
  iteration order cannot make the gate flaky.
- The generated sources are ordinary Go and assembly. **Commit them.**
- Pin the libclang and the C++ compiler versions. The parse decides which
  declarations and mangled names exist. The compiler decides whether the shim
  builds. A different libclang on the runner can legitimately produce different output for the same header. That is a real change to review.

<div class="callout callout-warn">
<span class="callout-title">The gate checks bytes</span>
<p>
A green <code>cppgo check</code> means "the generated sources match the headers
and the flags". The symbol itself is resolved at load time by <code>dlopen</code>/<code>dlsym</code>, or at
generation time when <code>-library</code> names a readable file. If you want CI
to prove the call works, add a test that opens the two objects and calls one binding. The repository's own end-to-end test does exactly that.
</p>
</div>

Next is [troubleshooting]({{< ref "manual/troubleshooting.md" >}}).
