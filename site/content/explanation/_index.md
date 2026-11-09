---
title: Explanation
summary: Understanding-oriented discussion of why cppgo is built this way.
weight: 40
description: Why the design looks like this, how it works, what it costs, and where it stops.
---

Explanation pages are read once, in order, by someone who wants to understand the
design rather than operate it. They answer the questions a user of the API does
not have to ask: why the pieces exist, what a call does to the Go runtime, how one
call crosses into C++, which C++ constructs are carried and how, what a call
costs, and what the mechanism cannot do. Each page stands alone on its own
subject; read in this order they build one model rather than six.

- [Why cppgo exists]({{< ref "explanation/why.md" >}}) — what cgo and a
  hand-written binding each cost, and what cppgo trades for what. Answers *should
  I use this at all?*
- [The Go runtime and a native call]({{< ref "explanation/go-runtime.md" >}}) — the
  model the design works around: what a goroutine, its stack, the scheduler and
  the collector do while a call into C++ runs. Answers *why is the design not
  simpler than this?*
- [Architecture]({{< ref "explanation/architecture.md" >}}) — the artefacts one
  generation produces, and one `mathx::Vector::at` call followed from the
  generated Go method into C++ and back. Answers *how does a call work?*
- [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}) — which
  of the three carriers a declaration gets and why, and which C++ constructs the
  generator supports, refuses, or has designed but not built. Answers *will my
  declaration bind, and how will it be called?*
- [Performance]({{< ref "explanation/benchmarks.md" >}}) — what one call costs,
  scenario by scenario, against cgo and plain Go, with the code of every arm and
  the runs the numbers come from. Answers *what does this cost me?*
- [Limits]({{< ref "explanation/limits.md" >}}) — the hard limits of the platform,
  the constructs this build refuses, what is designed but not shipped, and what
  only looks like a limit. Answers *what can this not do?*
