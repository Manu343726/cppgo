+++
title = "Tutorials"
description = "Four complete narratives with runnable code: a first binding, stateful objects and lifetimes, bulk data through pointer/length pairs, and what to do when the generator refuses."
weight = 30
+++

The tutorials build on each other, but each one stands alone. Every step shows
the command and the output you should see, so you can tell whether you are on
track before moving on.

1. **[Your first binding]({{< ref "tutorials/first-binding.md" >}})** — one free
   function, from header to running program. The smallest end-to-end path.
2. **[Stateful objects]({{< ref "tutorials/stateful-objects.md" >}})** —
   constructors, destructors, handles and the storage the generated package
   owns.
3. **[Bulk data]({{< ref "tutorials/bulk-data.md" >}})** — slices as
   `(pointer, length)`, and why a call per element is the wrong shape even when
   the boundary is cheap.
4. **[When the generator refuses]({{< ref
   "tutorials/when-generation-refuses.md" >}})** — read the error, then choose
   between moving the definition out of line, changing the signature, or falling
   back to shim mode.

They assume a Linux/amd64 machine with Go 1.25+, libclang and a C++17 compiler,
as described in [installation]({{< ref "manual/installation.md" >}}). If you
only read one, read the first.
