+++
title = "Project"
description = "Why the boundary is shaped this way, how a call works, what it costs, what it cannot do yet, and the questions that come up first: the rationale, architecture, benchmarks, scope and FAQ."
weight = 50
+++

The manual tells you how to use cppgo. This section is about the thing itself. It explains
why the boundary is shaped the way it is, how the generated artefacts fit
together, what it costs, what it does not do yet, and the four questions that
come up every time someone sees the project.

- **[Why cppgo]({{< ref "project/why.md" >}})** — for a Go developer new to
  FFI: what cgo costs and why, what a C++ library has that cgo cannot reach,
  what cppgo does instead, and when you should not use it.
- **[How it works]({{< ref "project/architecture.md" >}})** — the three
  artefacts, the three carriers, the callback ABI, the trampoline and the
  loader.
- **[Benchmarks]({{< ref "project/benchmarks.md" >}})** — the measured table,
  with the clock caveat, the carrier comparison, and how to reproduce it.
- **[Limitations and roadmap]({{< ref "project/limitations.md" >}})** — the v4
  scope, what is designed but not emitted yet, and the platform limits.
- **[FAQ]({{< ref "project/faq.md" >}})** — why not cgo, why not SWIG, why purego
  for libclang, why not shared memory always.
