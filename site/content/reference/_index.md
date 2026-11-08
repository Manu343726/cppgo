---
title: Reference
summary: Information-oriented descriptions of the CLI, the generated API and the runtime.
weight: 30
description: The exact commands, signatures and tables, without a narrative.
---

Reference pages are for looking something up, not for reading end to end. They
describe the surface you use — the command, the package `cppgo gen` writes, the
runtime it imports — and they say so when a row is designed rather than shipped.

- [Command line]({{< ref "reference/cli.md" >}}): `cppgo gen`, `build`, `check`
- [Generated API]({{< ref "reference/generated-api.md" >}}): the shape of the package `cppgo gen` writes
- [Runtime]({{< ref "reference/runtime.md" >}}): the `cppgo` package a generated binding imports

How a call reaches C++, and what each choice costs, is in
[Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
