---
title: "Installation"
description: "Requirements, installing the cppgo generator, adding the runtime dependency, and building the C++ side."
weight: 10
---

## Requirements

| Requirement | Why |
|---|---|
| **Go 1.25 or newer** | `go.mod` pins the minimum language version. |
| **Linux/amd64** | The only platform with trampolines in v1. The package builds elsewhere and panics with a clear message. |
| **A C++17 compiler** | Only to build *your* library, never to build the Go side. |

No C toolchain is involved in building the Go program. That is the whole point:
`CGO_ENABLED=0` is a supported configuration, and it is what the project's own
tests run under.

## 1. Install the generator

```console
$ go install github.com/Manu343726/cppgo/cmd/cppgo@latest
```

That puts a `cppgo` binary in `$(go env GOBIN)`, or in `$(go env GOPATH)/bin`
when `GOBIN` is unset. If the command is not found afterwards, that directory
is not on your `PATH`.

To build from a checkout instead:

```console
$ git clone https://github.com/Manu343726/cppgo
$ cd cppgo
$ make build      # compiles the packages and cmd/cppgo into ./bin/
```

## 2. Add the runtime dependency

The generated code imports the `cppgo` package for `ProbeStack`, `Ptr`,
`StrPtr`, `LastError` and the wire types, so the module needs it too:

```console
$ go get github.com/Manu343726/cppgo
```

`cppgo` has exactly one third-party dependency: `github.com/ebitengine/purego`,
used **only** for `dlopen`/`dlsym`/`dlclose`. Its calling convention is never
used.

## 3. Build your C++ library

cppgo does not build C++. You do, with whatever flags your project already
uses, and you ship a shared object:

```console
$ g++ -O2 -std=c++17 -fPIC -shared -o libmathx.so mathx.cpp
```

Two requirements on that build:

- It must export the symbols the generated `cppgo_shim.h` declares. Those
  declarations are wrapped in `extern "C"`, so the names are unmangled.
- It must export `cppgo_last_error` if any bound function can fail. A library
  that cannot fail does not need one — `cppgo.LastError` treats the symbol as
  optional.

A default-visibility build already does both. If you compile with
`-fvisibility=hidden`, mark the shim entry points and `cppgo_last_error` as
visible.

## 4. Put the library where `dlopen` can find it

The library is loaded at runtime, so its location is a deployment decision, not
a build-time one. Any of these work:

- an absolute path passed to the open call,
- a name on the dynamic loader's search path, such as `libmathx.so` with that
  directory in `LD_LIBRARY_PATH` or in a file under `/etc/ld.so.conf.d/`,
- a relative path containing a slash, resolved from the working directory.

For a program you ship as a single binary, the usual arrangement is to place
the `.so` beside the binary and open it by absolute path, so the process never
picks up a stale copy from a system directory.

## 5. Verify the toolchain

The quickest end-to-end check is the project's own test and benchmark path:

```console
$ make test       # CGO_ENABLED=0 pass, then the -race pass
$ make bench      # the numbers quoted in the docs
```

`make test` deliberately runs two passes. `CGO_ENABLED=0 go test -race` is
impossible — the toolchain rejects it with `-race requires cgo` — so the
no-cgo contract is proven by one pass and the race detector by another. Neither
replaces the other.

## Upgrading

The generator and the runtime are versioned together but the generated files
are yours to keep. After upgrading, re-run `cppgo gen` in each bound package so
the assembly matches the current `Args` layout and trampoline, then rebuild.
Stale generated code is the most likely cause of a confusing failure after an
upgrade.
