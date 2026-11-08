+++
title = "Examples"
description = "Two complete C++ libraries bound with the v4 generator: mathx, a small class, and stats, arithmetic over a large []float64 with most calls on the direct carrier."
weight = 40
+++

Both directories hold a real C++ library and the Go package the generator
produces from its header. They are the fastest way to see the whole shape
without writing it first.

**Audience:** you learn by reading a complete package before writing one.

- [**mathx**]({{< ref "examples/mathx.md" >}}): one small class. A constructor,
  `Close`, the copy constructor, `const` accessors, an inline accessor, a slice
  parameter, a mutator, a static member, a POD by value, and two members that
  throw. The report shows one direct binding and twelve thunks.
- [**stats**]({{< ref "examples/stats.md" >}}): arithmetic over `[]float64`.
  Four `noexcept` functions take the direct carrier and return no error.
  `histogram` throws, so it goes through the shim and returns an error. A
  pointer identity check proves the slice was not copied.

Each page shows the header, the exact `make gen` invocation, the carrier report
and the generated API. The `Makefile` in each directory owns the commands:

```console
$ cd examples/mathx
$ make gen     # build the library, then cppgo gen
$ make test    # test and vet the generated package
$ make check   # prove the committed generated files are current
```

`make check` is the CI shape. It regenerates the sources in memory and compares
them with what is committed. The examples build their own `cppgo` from
`../../cmd/cppgo`, so what runs is the generator next door.
