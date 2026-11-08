+++
title = "cppgo"
description = "Generate an idiomatic Go API from C++ headers: no cgo, no import \"C\", and no hand-written bindings. Every declaration gets a carrier, and cppgo compiles the shim itself."
weight = 0
+++

**cppgo calls C++ from Go with no cgo at build time and no `import "C"`.** You
point it at the C++ headers and the compilation flags your build already uses.
It parses them with libclang, generates the Go package a Go programmer would
have written, and compiles a C++ shim for the constructs the direct call cannot
express. You write no bindings.

Each bound declaration gets one **carrier**. A function that is `noexcept`, not
virtual and register-representable is called by a generated `NOSPLIT` assembly
trampoline straight into the C++ ABI. Everything else goes through a generated
`extern "C"` **thunk**, which cppgo compiles into `libcppgo_<package>.so` and
links against your library. A signature that passes a Go function to C++ uses a
third, callback-capable carrier.

A direct call costs 15.9 ns/op, measured 2026-11-08 with `go test -bench .
-benchtime 200000x`, against 151 ns/op for the reentrant carrier. The
[benchmarks page]({{< ref "project/benchmarks.md" >}}) has the tables, the
pinned-clock caveat and the command that produced them. What the trampoline does,
and why the `P` is never released, is [How it
works]({{< ref "project/architecture.md" >}}).

## What it handles

- classes with constructors, destructors, `Close` and methods.
- virtual dispatch, inline bodies, `std::string` and `std::vector`, records by
  value, and exceptions, all through the generated shim.
- callbacks from C++ into Go, through the reentrant carrier.
- concrete template specialisations, named with `-instantiate`.

The limits are on the [limitations page]({{< ref "project/limitations.md"
>}}). Linux/amd64 is the only target that executes a binding. A binary that
loads a shared object is dynamically linked. An exception with no C++ frame
terminates the process.

Start with the [why page]({{< ref "project/why.md" >}}) if you want the
argument, the [manual]({{< ref "manual/_index.md" >}}) if you want the whole
contract, the [quickstart]({{< ref "manual/quickstart.md" >}}) if you want a
working binding now, or the [tutorials]({{< ref "tutorials/_index.md" >}}) if
you prefer to learn by building one.
