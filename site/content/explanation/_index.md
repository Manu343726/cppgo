---
title: Explanation
summary: Understanding-oriented discussion of why cppgo is built this way.
weight: 40
description: Why the design looks like this, how it works, what it costs, and where it stops.
---

Explanation pages are read once, in order, by someone who wants to understand the
design rather than operate it. They explain the functionality: how a call
reaches C++, what it costs, and where it stops.

- [Why cppgo exists]({{< ref "explanation/why.md" >}})
- [The Go runtime and a native call]({{< ref "explanation/go-runtime.md" >}}) — the model the design works around
- [Architecture]({{< ref "explanation/architecture.md" >}}) — how it works
- [Carriers and the construct table]({{< ref "explanation/carriers.md" >}})
- [Performance]({{< ref "explanation/benchmarks.md" >}}) — the cost of a call
- [Limits]({{< ref "explanation/limits.md" >}})
