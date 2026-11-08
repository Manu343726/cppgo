+++
title = "Manual"
description = "The complete contract: installation, a worked quickstart, the generation input, the type mapping, the register convention, errors, object lifetimes, validation, CI and a glossary."
weight = 10
+++

The manual is the reference you keep open while binding a real library. It
starts with what the tool needs on the machine, walks one class from header to
call site, and then documents every rule the generator enforces — including the
constructs it refuses today.

**Audience:** a Go developer with a C++ library to bind, who has never built an
FFI. Terms are defined where they first appear and collected in the
[glossary]({{< ref "manual/glossary.md" >}}); a C++ construct you do not
recognise is one table row away from a definition.

Read it in order the first time. After that, the three pages you will come back
to are [the generation input]({{< ref "manual/manifest.md" >}}), [the type
mapping]({{< ref "manual/type-mapping.md" >}}) and [troubleshooting]({{< ref
"manual/troubleshooting.md" >}}).

If you want the exact signatures of the Go package, the [code
reference]({{< ref "reference/_index.md" >}}) is the other half of this
document: the manual explains what the tool does, the reference explains what
the generated code and the runtime package are.
