+++
title = "Installation"
description = "Go 1.25+, Linux/amd64, libclang for the generator and a C++17 compiler for your own library. What to install, how cppgo finds libclang, and how to verify the toolchain."
weight = 10
+++

## Requirements

| requirement | why |
|---|---|
| **Go 1.25 or newer** | the module pins `go 1.25.0`; CI tests stable and `1.25.x` |
| **Linux/amd64** | the generated trampolines are amd64 assembly with the System V ABI. The package still builds for other targets and panics with a clear message |
| **libclang** | the generator parses your C++ header with it. Install your distribution's libclang development package (`libclang-dev` is the name the tool's own error message suggests) |
| **a C++17 compiler** | to build *your* library. It is not used to build the Go side and cppgo never invokes it |

A C++17 compiler is a requirement of the library you are binding, not of cppgo.
cppgo itself is a Go program with no cgo; the C++ toolchain only matters when
you build the `.so` that the generated code will `dlopen`.

## Install the generator

```console
$ go install github.com/Manu343726/cppgo/cmd/cppgo@latest
```

From a checkout of the repository, `make build` produces the same binary at
`bin/cppgo` (it builds every command with `CGO_ENABLED=0`).

Released binaries are published on the
[releases page](https://github.com/Manu343726/cppgo/releases) for
`linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64` — those are the
targets the release workflow cross-compiles the CLI for. The runtime library is
still Linux/amd64 only in this version, so those other binaries cannot execute a
binding.

Verify the command is on your `PATH`:

```console
$ cppgo version
cppgo dev
```

A locally built or `go install`ed binary reports `dev` unless it was built with
`-ldflags "-X main.version=v1.2.3"`.

## Let cppgo find libclang

`internal/cxx` loads libclang at run time through purego — no cgo, no headers
needed. It searches, in order:

1. the bare names `libclang.so` and `libclang.dylib` (so the dynamic loader's
   own search path applies),
2. `/usr/lib`, `/usr/lib64`, `/usr/local/lib`, `/usr/lib/x86_64-linux-gnu`,
   `/lib/x86_64-linux-gnu` and `/usr/lib/aarch64-linux-gnu`,
3. versioned and per-LLVM layouts: `/usr/lib/libclang-*.so*`,
   `/usr/lib/llvm-*/lib/libclang.so*`, and the Homebrew paths on macOS.

If it cannot find one, every entry point returns an error wrapping
`cxx: libclang not found` with the list of paths it tried:

```console
$ cppgo gen -dir ./bindings/mathx
cppgo gen: cppgo: include/mathx/vector.hpp: cxx: libclang not found (searched
libclang.so, /usr/lib/libclang.so, ...; install libclang-dev or set CPPGO_LIBCLANG)
```

Set `CPPGO_LIBCLANG=/path/to/libclang.so` to point it at a specific library.
The search runs once per process; changing the variable takes effect in a new
process.

<div class="callout callout-warn">
<span class="callout-title">A header means libclang</span>
<p>
The direct ABI mode is validation: without libclang there is nothing to
validate against, so <code>cppgo gen</code> fails instead of guessing. If you
cannot install libclang on the build machine, use the shim mode deliberately:
<code>//cppgo:mode shim</code>, no <code>//cppgo:header</code>, and
<code>//cppgo:external</code> declarations.
</p>
</div>

## Build the C++ library you are binding

cppgo does not compile C++. Build a normal shared object with your own build
system. With a plain compiler:

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Iinclude -o libmathx.so src/mathx.cpp
```

The important constraint is not the flags — it is that the members you bind
must be **defined out of line**. A member defined inside its class definition is
implicitly inline and emits no dynamic symbol, so there is nothing for `dlsym`
to find. The generator detects that and refuses the binding; see
[validation]({{< ref "manual/validation.md" >}}).

## The Go side needs no C toolchain

Your bindings package is ordinary Go:

```console
$ CGO_ENABLED=0 go build ./...
```

That works even though the program will load a C++ shared object at run time.
`dlopen`/`dlsym` come from purego, which implements them without cgo.

<div class="callout">
<span class="callout-title">One honest consequence</span>
<p>
A Go binary that loads shared libraries at run time is <strong>dynamically
linked by definition</strong>. The claim is "no C toolchain, no
<code>import "C"</code>, no cgo", not "a static single file".
</p>
</div>

## Check that everything is in place

Run the generator once against a package you already have, or start from the
[quickstart]({{< ref "manual/quickstart.md" >}}). A successful run prints a line
per binding on stdout:

```
mathx::Vector::Vector              mangled _ZN5mathx6VectorC1Edd    abi from compile_commands.json
mathx::Vector::norm                mangled _ZNK5mathx6Vector4normEv abi from compile_commands.json
```

and writes `zz_cppgo.gen.go` and `zz_cppgo_amd64.s` next to your declarations.
If the flags were guessed because there was no `compile_commands.json` or
`.clangd`, the source says `abi from fallback flags (assumed)` and a warning
goes to stderr. That is not an error, but it means the parse may have missed
include paths; [compilation flags]({{< ref "manual/validation.md" >}}) explains
how to fix it.

Next: [quickstart]({{< ref "manual/quickstart.md" >}}).
