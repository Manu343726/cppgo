+++
title = "Tutorials"
description = "Four complete narratives with runnable code: a first binding, stateful objects and Close, bulk data and standard-library results, and what to do when generation refuses a construct."
weight = 30
+++

The tutorials build on each other, but each one stands alone. Every step shows
the command and the output you should see, so you can tell whether you are on
track before moving on.

**Audience:** a Go developer binding a C++ library for the first time. The
terms are defined in the [glossary]({{< ref "manual/glossary.md" >}}).

1. **[Your first binding]({{< ref "tutorials/first-binding.md" >}})** — one free
   function, from header to running program. The smallest end-to-end path, and
   the experiment that shows why `noexcept` changes the Go signature.
2. **[Stateful objects]({{< ref "tutorials/stateful-objects.md" >}})** —
   `NewVector`, `Close`, the `cppgo.Object` behind them, copying versus
   `Clone`.
3. **[Bulk data]({{< ref "tutorials/bulk-data.md" >}})** — slices as
   `(pointer, length)`, `std::vector` results copied into fresh slices, and why a
   call per element is the wrong shape even when the boundary is cheap.
4. **[When generation refuses]({{< ref
   "tutorials/when-generation-refuses.md" >}})** — read the error, tell a
   construct this build does not emit from a manifest typo from a compiler
   rejection, and pick the fix.

They assume a Linux/amd64 machine with Go 1.25+, libclang and a C++ compiler,
as described in [installation]({{< ref "manual/installation.md" >}}). If you
only read one, read the first.
