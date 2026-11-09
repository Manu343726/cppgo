---
title: "Ship a program that uses a binding"
description: "Two C++ files, one directory, and the ways to tell the generated package where they are."
weight: 30
---

This guide is for the moment after the binding works on your machine: you have
a Go binary and two C++ files, and you need them on another machine, in a
container, or in a package you hand to somebody else.

A program that uses a generated package loads **two** C++ files at run time: the
one cppgo built for you, and the C++ library it was linked against. The task is
to get those two files to the machine and tell the program where they are.

Both are *shared objects* (`.so` on Linux): files the operating system loads into
the process at run time. They are not linked into the Go binary.

The scenario is the `full` example. Its program, in
[`cmd/app/main.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/main.go),
opens the generated file by a relative path and points the package at its library
first, because the example's `Makefile` keeps the library under `build/` and the
shim under `gen/`. Your own deployment can keep the two in one directory instead,
as Recipe A does.

## Where the generated package looks

Every generated package has the same entry point, from
[`examples/full/gen/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/gen/zz_cppgo.gen.go):

```go
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
//
// path names the shim cppgo compiled beside this package. A path of "" uses the
// name the package was generated with. It is safe to call Open more than once;
// the first successful call wins.
func Open(path string) error
```

`Open(path)` opens the file cppgo built, then resolves the bindings. Your
own library is looked for in this order, and the first match wins:

1. `CPPGO_LIBRARY_PATH` (the environment);
2. `CppgoLibraryPath` (the package variable);
3. the directory the generated file was loaded from, when it was opened by a path
   that has one and your library sits there;
4. the recorded name, resolved by the dynamic loader's normal search.

Points 1 and 2 are read when the package is opened. Set them before the first
call; a later change is not observed.

`Open("")` uses the name the package was generated with, which the loader
resolves through its own search path — **not** the current directory.

The mechanism behind this order, and the names recorded in the files, is in
[Architecture]({{< ref "explanation/architecture.md" >}}#loading-the-two-objects).

## Recipe A: keep the pair together

The simplest deployment is one directory with the binary and both files:

```
/opt/mytool/
  mytool
  libcppgo_mytool.so
  libmylib.so.1
```

Open the generated file by a path that contains a slash, and your library is
found beside it:

```go
if err := mylib.Open("/opt/mytool/libcppgo_mytool.so"); err != nil {
	log.Fatal(err)
}
```

A relative path containing a slash (`./libcppgo_mytool.so`) is also used as
given, so running from the directory works during development.

## Recipe B: a loader search path

Put the directory on the loader's search path and open the generated file by its
name:

```console
$ export LD_LIBRARY_PATH=/opt/mytool/lib
```

`LD_LIBRARY_PATH` works for both files, because the loader searches it for the
name the package was generated with and for your library's name.

## Recipe C: name the library explicitly

When your library lives somewhere else, name it. The environment variable works
for a deployment that cannot change the program:

```console
$ CPPGO_LIBRARY_PATH=/srv/mylib/libmylib.so.1 ./mytool
```

or in the program:

```go
mylib.CppgoLibraryPath = "/srv/mylib/libmylib.so.1"
if err := mylib.Open("/opt/mytool/libcppgo_mytool.so"); err != nil {
	log.Fatal(err)
}
```

Both make the package load that path explicitly instead of relying on the
recorded name. The generated file still refers to your library by name, so keep
the library where the loader can also find it: beside the generated file, or on
the search path.

## What the failures mean

| Message | Meaning |
|---|---|
| `cppgo: open the shim …: cannot open shared object file` | the path passed to `Open` is wrong, or the generated file is missing |
| `cppgo: open the shim …: libmylib.so.1: cannot open shared object file` | the generated file was found, but your library was not: it is not beside it and not on the search path |
| `the shim cppgo compiled is stale; run cppgo gen again` | the Go package and the C++ files are from different generator runs; regenerate |
| `cppgo: symbol "…": not found` | the library that was loaded does not export that function: the wrong build, or the wrong library |

## Moving a build between machines

The C++ side is built on the machine that generates it, with the flags of your
project. Copying it to a machine with a different C++ runtime is the same class
of problem as copying any other C++ binary: build it there, or ship the runtime
it needs.

## Removing the library half of the problem

`cppgo.OpenFS` lets the C++ files travel inside the Go binary: they are embedded
with `//go:embed`, extracted to a private per-user cache, and loaded from there.
It is implemented and tested in the runtime; the generated package does not call
it yet. See
[Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Related

- The runtime functions used here: [Runtime]({{< ref "reference/runtime.md" >}}).
- How the two files are found at run time: [Architecture]({{< ref "explanation/architecture.md" >}}#loading-the-two-objects).
