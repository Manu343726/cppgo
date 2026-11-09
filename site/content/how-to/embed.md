---
title: "Embed the C++ side in the Go binary"
description: "OpenFS and go:embed: what works today, and what the generated Open does not do yet."
weight: 40
---

Normally a program that uses a binding ships its binary and two C++ files that
have to stay together — the shim and the library it calls. The four ways to point
the generated package at those two files are in
[Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}#where-the-generated-package-looks).
`cppgo.OpenFS` removes the question: it takes a file system of embedded files,
writes them once into a cache private to your user, and loads one of them by
name. The C++ side then travels inside the Go binary as data, and is unpacked
before it is loaded.

Use it when the program must arrive as a single file and you control the code
that opens the library. The scenario is a small tool that embeds its shim — the
C++ library cppgo builds — and the library it calls, then loads both from the
embedded file system at start-up, which is what the example below does.

`OpenFS` is a runtime function today: the package `cppgo gen` writes does not
call it yet, and a generated `Open` still loads the two files from the
filesystem. So this guide is for code that uses the runtime directly; a generated
binding deploys as described in
[Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).

## The call

```go
func OpenFS(fsys fs.FS, name string) (*Library, error)
```

`fsys` is any `fs.FS`, Go's interface for a read-only file tree — an `embed.FS`
in production, `fstest.MapFS` or `os.DirFS` in tests. `name` is the base name of
the file to load, and it must be one of the shared objects in `fsys`.

A *shared object* is a file a C++ compiler produced with `-shared`, named `.so` on
Linux, which the operating system maps into a process at run time instead of
copying its code into the executable. `libmylib.so.1` is one: the `.1` is the
version part of its *soname*, the short name a shared object records inside
itself when it is built, so that a program can depend on the name rather than on
a path.

Here is a program that embeds its two objects and opens the shim:

```go
package main

import (
	"embed"

	"github.com/Manu343726/cppgo"
)

//go:embed lib/libcppgo_mytool.so lib/libmylib.so.1
var libs embed.FS

func main() {
	lib, err := cppgo.OpenFS(libs, "libcppgo_mytool.so")
	if err != nil {
		// *cppgo.OpenError, *cppgo.ExtractError, *cppgo.DependencyError
		// or cppgo.ErrInsecureCache
		panic(err)
	}
	defer lib.Close()

	// Resolve the symbol you need, then call it through the runtime.
	if _, err := lib.Symbol("mylib_add"); err != nil {
		panic(err)
	}
}
```

`//go:embed` is a Go build directive: it copies the files it names into the
binary at build time and makes them readable through an `embed.FS`, so `libs`
holds both objects as bytes and nothing has to sit beside the binary. The
`//go:embed` line has to be immediately above the variable, and the variable
must be of type `embed.FS` (or `string`/`[]byte` for one file).

Embed **every** object the program should carry: the shim, and your library.
`OpenFS` writes out every shared object in `fsys`, not only the `name` you ask
for, and loads the set in dependency order so the shim's dependency resolves to
the copy it just unpacked. A dependency you leave out is looked for by the
dynamic loader instead — fine for `libc` and `libstdc++`, which the system
provides, and a failure for a library that exists only on your build machine.
Entries may sit in a subdirectory; they are all written into one directory by
base name, so two entries with the same base name are an error. Other files are
ignored.

If your own library has its own C++ dependencies beside it, link it with
`-Wl,-rpath,$ORIGIN` so it finds them where it is unpacked. `-Wl,` passes the
rest of the argument to the linker, `-rpath` records a *run path* — a list of
directories the loader searches for that object's dependencies — and `$ORIGIN`
in that list means "the directory this object is in". cppgo does this for the
file it builds.

## What the cache gives you

- the unpacked files live under `os.UserCacheDir()/cppgo/`, in a directory
  private to your user, and are reused while they are unchanged.
  `os.UserCacheDir()` is Go's per-user cache directory (`$XDG_CACHE_HOME`, or
  `~/.cache` when that is unset);
- a corrupted or replaced file is detected and rewritten: every file is hashed
  when the set is scanned, the set's digest names the cache directory, and a
  file's digest decides whether the copy already there is still good;
- a cache path another local user could have tampered with is refused with
  `cppgo.ErrInsecureCache` rather than used.

The `*cppgo.ExtractError` a failed unpack returns has a `Name` (the object), a
`Dir` (the cache directory, empty when none was written) and an `Err` (the file
system's own failure). `*cppgo.DependencyError` is the one to know about when a
set is incomplete: it names the object and the missing dependency, and says that
neither the embedded set nor the loader's search provides it
([Runtime]({{< ref "reference/runtime.md" >}}#errors) has the fields).

The layout, the digest and the permission rules are in
[Architecture]({{< ref "explanation/architecture.md" >}}#embedded-objects).

## Escape hatches

| What | Effect |
|---|---|
| `CPPGO_LIBRARY_PATH=<path>` | `OpenFS` loads from that path and unpacks nothing. The value is one object, or a list of entries separated like `PATH`; the first entry that resolves wins — a directory is searched for `name`, and an entry that is a file is used as it is. It is the development override: point the same program at a library your C++ build left in place. Because nothing is unpacked, the rest of the set is not extracted either, so the object's dependencies must come from the loader's search |
| `cppgo.CacheDir()` | names the cache directory |
| `cppgo.CleanCache()` | removes everything `OpenFS` has unpacked. Clearing the cache costs one extraction, never correctness |

When `CPPGO_LIBRARY_PATH` resolves — to the object itself, or to a directory that
holds it — no extraction happens, so no `ExtractError` and no
`ErrInsecureCache` can happen: the function returns what `cppgo.Open` returns,
which is `*cppgo.OpenError` when the loader cannot open it.

## What embedding does not buy

The Go binary still needs the platform's C and C++ runtimes: the C++ objects
inside it declare `libstdc++.so.6`, `libm.so.6`, `libgcc_s.so.1` and `libc.so.6`
as dependencies, and embedding does not change that. Those are the C++ standard
library, the C maths library, GCC's runtime support and the C library itself;
every C++ program on the system uses the same copies. A program that must run
where libstdc++ is absent needs a static build of the C++ side instead, which is
outside this design.

The cache protects against *other local users* replacing a file the program
loads. It offers no defence against a process running as the same user, or
against an attacker who can already write that user's cache directory.

## Related

- The rest of the runtime: [Runtime]({{< ref "reference/runtime.md" >}}).
- How the cache and the extraction work: [Architecture]({{< ref "explanation/architecture.md" >}}#embedded-objects).
