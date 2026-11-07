+++
title = "Examples"
description = "Two complete, runnable bindings in the repository: mathx, a stateful C++ class, and stats, arithmetic over a million-element []float64."
weight = 40
+++

Both examples are real packages in the repository, with their own `go.mod`, a
`Makefile` that builds the C++ library and runs the generator, tests, and
committed generated files. They are the fastest way to see the whole shape
without writing it first.

- **[mathx]({{< ref "examples/mathx.md" >}})** — a small C++ class bound
  directly: constructor, destructor, `const` methods, a slice parameter, a static
  member, and the inline member the generator refuses on purpose.
- **[stats]({{< ref "examples/stats.md" >}})** — arithmetic over `[]float64`:
  the same data crossing as `(pointer, length)` in both directions, a pointer
  identity check, and an `int32_t` status turned into a Go error.

```console
$ cd examples/mathx && make
$ cd examples/stats && make && make bench
```

Each directory builds its own copy of the generator from `../../cmd/cppgo`, so
what you run is the code next door, not a binary from `PATH`.
