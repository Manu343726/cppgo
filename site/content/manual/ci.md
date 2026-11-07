+++
title = "cppgo check in CI"
description = "Make the generated bindings a checked artifact: what cppgo check compares, its exit codes, a GitHub Actions job that fails on stale bindings, and how to keep libclang available on the runner."
weight = 90
+++

## What `check` does

`cppgo check` is `cppgo gen` with the writing turned off. It regenerates every
file in memory, compares it byte for byte with what is on disk, and reports the
difference:

```console
$ cppgo check -dir ./bindings/mathx
./bindings/mathx is out of date (run "cppgo gen"): zz_cppgo.gen.go, zz_cppgo_amd64.s
$ echo $?
1
```

When everything is current it prints one line and exits 0:

```console
$ cppgo check -dir ./bindings/mathx
./bindings/mathx: generated files are up to date
```

Exit codes are the CLI's usual ones:

| code | meaning |
|---|---|
| `0` | generated files are up to date, or the command succeeded |
| `1` | generation failed, or a file is missing / stale |
| `2` | the command line itself was wrong (unknown flag, bad `-dir`, extra argument) |

`-q` suppresses the success message, which is what a CI log wants. The failure
message still goes to stderr and still exits 1.

## Why check instead of gen in CI

`gen` rewrites your source files in place. On a CI runner that means a build
that "passes" while producing a diff nobody looks at, or a source tree that
changes under a test. `check` is the read-only form: it proves that the
committed generated code is exactly what the current directives and header
produce. If someone edits a Go body or a directive and forgets to regenerate,
the job fails and the error names the stale files.

## A GitHub Actions job

The bindings package needs libclang, because the direct ABI mode resolves and
validates every binding against the header. The C++ compiler is *not* needed to
run the generator; it is needed only if the same job also builds the `.so` the
tests load. This job does both.

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
        run: cppgo check -dir ./bindings/mathx -q
```

`-q` keeps the log to the failure. Without `-q` the success line is
`./bindings/mathx: generated files are up to date`.

If your project has a `compile_commands.json`, pass it so the parse uses the
same flags as the build. This is the difference between validating against the
real headers and validating against a guess:

```yaml
      - name: Check that the bindings are current
        run: cppgo check -dir ./bindings/mathx -q -compile-commands ./build
```

Without it, the generator may fall back to `-std=c++17 -I<header dir>` and print
the assumed-flags warning. That is not a failure by itself, but a header whose
own includes are missing will parse incompletely, and a binding that should have
been rejected might pass — or the reverse.

## Cache and reproducibility

- `cppgo check` is deterministic: it compares bytes. A timestamp or a random
  map iteration order in the output would make the gate unusable, so the
  emitter sorts everything.
- The generated files are ordinary Go and assembly sources. **Commit them.**
  They are what the package compiles; a checkout that regenerates on every build
  needs the generator, libclang and the C++ headers on every machine.
- Regenerating with a different libclang version can change which mangled name
  is chosen for the same declaration. One documented corner: for an abstract
  class, `clang++` emits only the base-object constructor `C2`, while libclang
  names `C1` (the complete-object one), and `g++` emits both. Pinning the
  generator and libclang versions is safer than regenerating in one place only.

<div class="callout callout-warn">
<span class="callout-title">The gate checks bytes, not behaviour</span>
<p>
A green <code>cppgo check</code> means "the generated files match the
directives". It does not mean the C++ library exports the symbol: that is
resolved at load time by <code>dlopen</code>/<code>dlsym</code>. If you want
CI to prove that too, add a test that opens the built
<code>.so</code> and calls one binding — which is what the repository's own
test suite does.
</p>
</div>

## Pre-commit, the same way

A pre-commit hook is the same command with a different failure UX:

```sh
#!/bin/sh
cppgo check -dir ./bindings/mathx -q || {
  echo "bindings are stale: run 'cppgo gen -dir ./bindings/mathx'" >&2
  exit 1
}
```

Next: [troubleshooting]({{< ref "manual/troubleshooting.md" >}}).
