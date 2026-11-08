+++
title = "Examples"
description = "Two complete C++ libraries bound with the v4 generator: mathx, a small class, and stats, arithmetic over a million-element []float64 with a status code."
weight = 40
+++

Both directories hold a real C++ library and the Go package the generator
produces from its header. They are the fastest way to see the whole shape
without writing it first.

**Audience:** you learn by reading a complete package before writing one.

<div class="callout">
<span class="callout-title">The directories are being regenerated</span>
<p>
On <code>main</code>, <code>examples/</code> still contains the v3 packages
(Go declarations with directives). Issue #4 regenerates them against the v4
emitter and rewrites their <code>Makefile</code>s. The pages below describe the
v4 packages generated from the same headers, with the commands and output taken
from real runs.
</p>
</div>

- **[mathx]({{< ref "examples/mathx.md" >}})** — one small class with a constructor,
  `Close`, copy constructor, `const` accessors, a slice parameter, a mutator,
  a static member, and the inline member v3 used to refuse (bound through a
  thunk in v4).
- **[stats]({{< ref "examples/stats.md" >}})** — arithmetic over `[]float64`. The same data crosses as `(pointer, length)` in both directions, a pointer
  identity check, and an `int32_t` status turned into a Go error with
  `-status`.

Each page shows the header, the exact `cppgo gen` invocation, the carrier report
and the generated API. The repository's `Makefile`s are the build once #4 lands.
until then, the commands on the pages are the v4 commands to run by hand.
