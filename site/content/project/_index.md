+++
title = "Project"
description = "How the call works, what it costs, what it cannot do yet, and the questions the design had to answer: benchmarks, limitations and roadmap, and the FAQ."
weight = 50
+++

The manual tells you how to use cppgo. This section is about the thing itself:
why the boundary is shaped the way it is, what it costs, what it does not do yet,
and the four questions that come up every time someone sees the project.

- **[How it works]({{< ref "project/architecture.md" >}})** — the path from a Go
  call to a C++ frame, the assembly trampoline, and why the `P` is not released.
- **[Benchmarks]({{< ref "project/benchmarks.md" >}})** — the measured table,
  with the clock caveat, and how to reproduce it.
- **[Limitations and roadmap]({{< ref "project/limitations.md" >}})** — arm64,
  Windows, by-value returns, callbacks, and the two ABI-inference tiers that are
  designed but not implemented.
- **[FAQ]({{< ref "project/faq.md" >}})** — why not cgo, why not SWIG, why purego
  for libclang, why not shared memory always.
