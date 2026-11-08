---
title: "Ship a program that uses a binding"
description: "Two shared objects, one load order, and the ways to point the generated package at them."
weight: 30
---

A program that uses a generated package loads **two** shared objects at run
time: the shim cppgo compiled, and the C++ library the shim was linked against.
This guide is about getting those two files to the machine and telling the
program where they are.

A *shared object* is a file the dynamic loader maps into the process when
something asks for it. It is not linked into the Go binary; it is found at run
time, by path or by name.

## What the generated code does

`mathx.Open(path)` does three things, in this order
([`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go)):

1. loads your library, if it can find it (a failure here is tolerated: the shim
   names it as a dependency, so loading the shim brings it in anyway);
2. loads the shim, and fails with `cppgo: open the shim …` if that fails;
3. resolves every symbol the package binds, and fails with
   `the shim cppgo compiled is stale; run cppgo gen again` when a thunk is
   missing.

The shim's `DT_NEEDED` entry names your library by its soname (or, when the
library declares none, by its file name), and cppgo links the shim with
`-Wl,-rpath,$ORIGIN` (`internal/gen/build.go`). `$ORIGIN` in a run path means
"the directory this object was loaded from", so **an object placed beside the
shim is found without any environment variable**.

Your library is looked for in this order, and the first match wins:

1. `CPPGO_LIBRARY_PATH` (the environment);
2. `CppgoLibraryPath` (the package variable);
3. the shim's own directory, when the shim was opened by a path that has one and
   the library sits there;
4. the recorded name, resolved by the dynamic loader's normal search.

Points 1 and 2 are read when the package is opened. Set them before the first
call; a later change is not observed.

`Open("")` uses the name the package was generated with, which the loader
resolves through its own search path — **not** the current directory.

## Recipe A: keep the pair together

The simplest deployment is one directory with the binary and both objects:

```
/opt/mytool/
  mytool
  libcppgo_mytool.so
  libmylib.so.1
```

Open the shim by a path that contains a slash, and the sibling is found:

```go
if err := mylib.Open("/opt/mytool/libcppgo_mytool.so"); err != nil {
	log.Fatal(err)
}
```

A relative path containing a slash (`./libcppgo_mytool.so`) is also used as
given, so running from the directory works during development.

Check that the pairing really is recorded in the shim rather than assumed:

```console
$ readelf -d /opt/mytool/libcppgo_mytool.so | grep -E 'NEEDED|RUNPATH'
 0x0000000000000001 (NEEDED)  Shared library: [libmylib.so.1]
 0x000000000000001d (RUNPATH) Library runpath: [$ORIGIN]
```

## Recipe B: a loader search path

Put the objects' directory on the loader's search path and open the shim by its
name:

```console
$ export LD_LIBRARY_PATH=/opt/mytool/lib
```

`LD_LIBRARY_PATH` is a deployment decision, not a cppgo mechanism: it works
because the loader searches it for both the shim name and the library name.

## Recipe C: name the library explicitly

When the library lives somewhere the shim cannot reach, name it. The environment
variable works for a deployment that cannot change the program:

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

Both do the same thing: they make the package load that path explicitly instead
of relying on the recorded name. Note what they do **not** do: the shim still
has a `DT_NEEDED` entry for the library's name, and the loader still has to find
a file for it. Keep the library where `$ORIGIN` or the search path can see it, or
the shim fails before your override is used.

## What the failures mean

| Message | Meaning |
|---|---|
| `cppgo: open the shim …: cannot open shared object file` | the shim path is wrong, or the shim is missing |
| `cppgo: open the shim …: libmylib.so.1: cannot open shared object file` | the shim was found but its dependency was not: the library is not beside it and not on the search path |
| `the shim cppgo compiled is stale; run cppgo gen again` | the Go package and the shim are from different generator runs; regenerate |
| `cppgo: symbol "…": not found` for a direct symbol | the library the shim loaded exports no such symbol: the wrong build, or the wrong library |

The last one is why the generator cross-checks direct bindings against the
library's dynamic symbol table when `-library` names a readable file.

## Moving a build between machines

The shim is compiled by cppgo with the flags discovered for the header
(`compile_commands.json` or `.clangd`), the compiler named by `-cxx` or found as
`g++`/`clang++`, and the same standard library as the library it links. Copying
the shim to a machine with a different C++ runtime is the same class of problem
as copying any other C++ binary: build it there, or ship the runtime it needs.

Two objects ship instead of one. That is the cost of the design: your library is
your build, and cppgo cannot merge into it.

## Removing the library half of the problem

`cppgo.OpenFS` extracts embedded objects into a private cache and loads one from
there, which is how the C++ side becomes a single file to ship. It is implemented
and measured in the runtime; the generated package does not call it yet. See
[Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Related

- The full loader contract: [Runtime]({{< ref "reference/runtime.md" >}}).
- Why two objects: [Design decisions]({{< ref "explanation/decisions.md" >}}).
