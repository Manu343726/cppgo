+++
title = "Reference"
description = "The cppgo package, the three generated files, and the command-line tool: exact signatures, generated anatomy and every flag."
weight = 20
+++

This section is the lookup half of the documentation. It has three pages:

- **[The `cppgo` package]({{< ref "reference/go-api.md" >}})** — every exported
  identifier, its exact signature, and the behaviour the documentation
  comments promise.
- **[The generated files]({{< ref "reference/generated-code.md" >}})** — what
  `zz_cppgo.gen.go`, `zz_cppgo_amd64.s` and the rewritten source contain, line
  by line, and why.
- **[The CLI]({{< ref "reference/cli.md" >}})** — `cppgo gen`, `check`,
  `version` and `help`, with every flag and every exit code.

The canonical API documentation is generated from the source and published at
[pkg.go.dev/github.com/Manu343726/cppgo](https://pkg.go.dev/github.com/Manu343726/cppgo).
When the two disagree, pkg.go.dev wins: it is the godoc comment on the code you
are importing, this page is the narrative around it.
