---
title: "Ship a program that uses a binding"
description: "Two C++ files, one directory, and the ways to tell the generated package where they are."
weight: 30
---

This guide is for the moment after the binding works on your machine: you have
a Go binary and two C++ files, and you need them on another machine, in a
container, or in a package you hand to somebody else.

A program that uses a generated package loads **two** C++ files at run time: the
one cppgo built for you — the *shim* — and the C++ library it was linked
against. The task is to get those two files to the machine and tell the program
where they are.

Both are *shared objects*: files a C++ compiler produced with the `-shared` flag,
named `.so` on Linux, which the operating system maps into the process at run
time instead of copying their code into the executable. They are not linked into
the Go binary. `cppgo.Open` asks for one through **`dlopen`**, the C library
function that loads a shared object and returns a handle to its symbols, and the
**dynamic loader** — the part of the C library that does the mapping and resolves
the names an object refers to (`ld-linux-x86-64.so.2` on this platform) — finds
it.

The scenario is the `full` example. Its program, in
[`cmd/app/main.go`](https://github.com/Manu343726/cppgo/blob/main/examples/full/cmd/app/main.go),
opens the generated file by a relative path and points the package at its library
first, because the example's `Makefile` keeps the library under `build/` and the
C++ side cppgo builds under `gen/`. Your own deployment can keep the two in one
directory instead, as Recipe A does.

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

`Open(path)` opens the file cppgo built, then resolves the bindings. Your own
library is looked for in this order, and the first match wins — the four
packaging choices, each with the deployment it suits:

| Order | Where it looks | Choose it when |
|---|---|---|
| 1 | `CPPGO_LIBRARY_PATH`, an environment variable | the deployment cannot change the program |
| 2 | `CppgoLibraryPath`, a package variable | the program knows where it put the library |
| 3 | the directory the shim was opened from, when `path` contains a slash and the library sits there | the default: ship the pair in one directory |
| 4 | the *recorded name*, resolved by the dynamic loader's own search | the library lives in a system directory |

The recorded name is the string the shim stores as its dependency: the
*soname* — the short name (`libmylib.so.1`) a shared object records inside itself
when it is built, so that programs can depend on the name rather than on a path.
A library built without a soname is recorded under its file name.

Points 1 and 2 are read when the package is opened. Set them before the first
call; a later change is not observed. `CPPGO_LIBRARY_PATH` holds one path here,
the library itself — the generated package does not split it into a list.

`Open("")` uses the name the package was generated with, which the loader
resolves through its own search path — **not** the current directory. That search
is, in order: `DT_RPATH` recorded in the object that is asking, and only when that
object has no `DT_RUNPATH`; `LD_LIBRARY_PATH`; the asking object's `DT_RUNPATH`;
the cache `/etc/ld.so.cache`, built by `ldconfig`; then `/lib` and `/usr/lib`.
`DT_RPATH` and `DT_RUNPATH` are the same kind of list, recorded in an object at
link time with `-rpath`; the difference is where they sit in that order. The shim
cppgo builds carries `DT_RUNPATH = $ORIGIN`, meaning "the directory this object is
in".

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
found beside it. `mylib` here is the generated package, imported under the name
its `-package` flag gave it:

```go
if err := mylib.Open("/opt/mytool/libcppgo_mytool.so"); err != nil {
	log.Fatal(err)
}
```

The file must be present under the *recorded* name — `libmylib.so.1`, the soname,
not `libmylib.so` (or the plain file name, when the library records no soname) —
because that is the name the package looks for. A relative path containing a slash
(`./libcppgo_mytool.so`) is also used as given, so running from the directory
works during development.

## Recipe B: a loader search path

Put the directory on the loader's search path and open the generated file by its
name:

```console
$ export LD_LIBRARY_PATH=/opt/mytool/lib
```

`LD_LIBRARY_PATH` is the one search-path variable the loader reads from the
environment, and it works for both files: the shim, because `Open("")` asks for
the generated name rather than a path, and your library, because the loader
resolves the shim's recorded dependency through the same list:

```go
if err := mylib.Open(""); err != nil {
	log.Fatal(err)
}
```

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
recorded name. Whether that is enough on its own depends on the soname:

- **the library records a soname** (the normal case, and what `SOVERSION` in
  CMake gives you): loading that path is enough. When the shim is opened
  afterwards, its recorded dependency matches the soname of the object already in
  the process, and the loader reuses it — the library does not have to be beside
  the shim or on any search path;
- **the library records no soname**: the shim's dependency is the bare file name,
  and the loader has nothing to match the already-loaded object against. Keep the
  library beside the shim, or put its directory on the search path, as in Recipe
  B.

## What the failures mean

The messages are chains: each layer names the call it was making and keeps the
layer below. A shim that is not where `Open` looked, in full:

```
cppgo: open the shim /opt/mytool/libcppgo_mytool.so: cppgo: open "/opt/mytool/libcppgo_mytool.so": rtl: dlopen in "/opt/mytool/libcppgo_mytool.so": /opt/mytool/libcppgo_mytool.so: cannot open shared object file: No such file or directory
```

Read the last clause first; it is the loader's own sentence. These are the ones
you will meet:

| Last clause | Meaning |
|---|---|
| `<the shim path>: cannot open shared object file: No such file or directory` | the path passed to `Open` is wrong, or the generated file is missing |
| `<libmylib.so.1>: cannot open shared object file: No such file or directory` | the shim was found, but your library was not: it is not beside the shim, not on the search path, and no override names it |
| `the shim cppgo compiled is stale; run cppgo gen again` | the Go package and the C++ shim are from different generator runs; regenerate |
| `undefined symbol: <name>` | the library that was loaded does not export that function: the wrong build, or the wrong library |

## Moving a build between machines

The C++ side is built on the machine that generates it, with the flags of your
project. Copying it to a machine with a different C++ runtime — a different
`libstdc++`, the C++ standard library every C++ program links against — is the
same class of problem as copying any other C++ binary: build it there, or ship the
runtime it needs.

## Removing the library half of the problem

`cppgo.OpenFS` lets the C++ files travel inside the Go binary: they are embedded
with `//go:embed` — a Go directive that copies the named files into the binary at
build time — extracted to a private per-user cache, and loaded from there.
It is implemented and tested in the runtime; the generated package does not call
it yet. See
[Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Related

- The runtime functions used here: [Runtime]({{< ref "reference/runtime.md" >}}).
- How the two files are found at run time: [Architecture]({{< ref "explanation/architecture.md" >}}#loading-the-two-objects).
