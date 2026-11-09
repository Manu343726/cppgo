---
title: Reference
summary: Information-oriented descriptions of the CLI, the generated API and the runtime.
weight: 30
description: The exact commands, signatures and tables, without a narrative.
---

Reference pages are for looking something up, not for reading end to end. They
describe the surface you use: the `cppgo` command, which turns a C++ header into
a Go package that calls the compiled library; the generated Go package itself;
and the `cppgo` package that package imports. They say so when a row is designed
rather than shipped.

| You want | Look up |
|---|---|
| the subcommands, their flags and exit codes, and the files a run writes | [Command line]({{< ref "reference/cli.md" >}}) |
| the Go type or function a C++ declaration becomes, and the C++ it came from | [Generated API]({{< ref "reference/generated-api.md" >}}) |
| the Go package a generated binding imports: loading, objects, callbacks and errors | [Runtime]({{< ref "reference/runtime.md" >}}) |

How a call reaches C++, and what each choice costs, is in
[Carriers and the construct table]({{< ref "explanation/carriers.md" >}}). For a
task rather than a lookup, the how-to guides start at
[Generate a binding for your own library]({{< ref "how-to/generate.md" >}}); to
learn the whole flow from the beginning, start at
[Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}}).
