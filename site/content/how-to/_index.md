---
title: How-to guides
summary: Task-oriented recipes for a specific problem you already have.
weight: 20
description: Recipes for a task, assuming you already know what you want to do.
---

Each guide answers one question and stops. None of them teaches cppgo from
scratch; the [tutorial]({{< ref "tutorials/first-binding.md" >}}) does that.

| You want to | Read | It starts from |
|---|---|---|
| generate a Go package for a C++ library you already build | [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}) | a header and the flags it is compiled with |
| bind a class or function template | [Bind a C++ template library]({{< ref "how-to/templates.md" >}}) | a template header and the Go program that uses it |
| ship the binary with its two C++ files | [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}) | a generated package that already runs on your machine |
| carry the C++ side inside the Go binary | [Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}) | a program whose source you control |
| know what a failing call returns | [Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}) | a bound call that can fail |
| gate the generated sources in a pipeline | [Gate a generated package in CI]({{< ref "how-to/ci.md" >}}) | a repository that commits them |

When you want the exact surface rather than a task — a flag, a generated
signature, an error type — the [reference]({{< ref "reference/_index.md" >}})
has it.
