---
title: "Embed the C++ side in the Go binary"
description: "OpenFS and go:embed: what works today, and what the generated Open does not do yet."
weight: 40
---

Normally a program that uses a binding ships its binary and two C++ files that
have to stay together. `cppgo.OpenFS` removes that: it takes a file system of
embedded files, unpacks them once into a cache private to your user, and loads
one of them by name. The C++ side then travels inside the Go binary.

**Status.** `OpenFS` is implemented and tested in the runtime. The package
`cppgo gen` writes **does not call it yet**: its `Open` loads the two files from
the filesystem, and connecting `OpenFS` to generated packages is recorded as
future work. So today this page is for code that uses the runtime directly; a
generated binding still deploys as described in
[Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).

## The call

```go
func OpenFS(fsys fs.FS, name string) (*Library, error)
```

`fsys` is any `fs.FS`: an `embed.FS` from `//go:embed` in production, and
`fstest.MapFS` or `os.DirFS` in tests. `name` is the base name of the file to
load, and it must be one of the `.so` entries.

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
		// *cppgo.OpenError, *cppgo.ExtractError or cppgo.ErrInsecureCache
		panic(err)
	}
	defer lib.Close()

	add, err := lib.Symbol("mylib_add")
	// ... use the library like any other cppgo.Library
}
```

Embed **every** `.so` the program loads, not just the one named in the call: the
generated file refers to your library, so both have to be extracted together.
Entries may sit in a subdirectory; they are unpacked into one directory by base
name, so two entries with the same base name are an error. Other files are
ignored.

If your own library has its own C++ dependencies beside it, link it with
`-Wl,-rpath,$ORIGIN` so it finds them where it is unpacked; cppgo does that for
the file it builds.

## What the cache gives you

- the unpacked files live under `os.UserCacheDir()/cppgo/`, in a directory
  private to your user, and are reused while they are unchanged;
- a corrupted or replaced file is detected and rewritten;
- a cache path another local user could have tampered with is refused with
  `cppgo.ErrInsecureCache` rather than used.

The layout, the digest and the permission rules are in
[Architecture]({{< ref "explanation/architecture.md" >}}#embedded-objects).

## Escape hatches

| What | Effect |
|---|---|
| `CPPGO_LIBRARY_PATH=<path>` | `OpenFS` loads that path and unpacks nothing. It is the development override: point the same program at a library your C++ build left in place |
| `cppgo.CacheDir()` | names the cache directory |
| `cppgo.CleanCache()` | removes everything `OpenFS` has unpacked. Clearing the cache costs one extraction, never correctness |

## What embedding does not buy

The Go binary still needs the platform's C and C++ runtimes: the C++ objects
inside it declare `libstdc++.so.6`, `libm.so.6`, `libgcc_s.so.1` and `libc.so.6`
as dependencies, and embedding does not change that. A program that must run
where libstdc++ is absent needs a static build of the C++ side instead, which is
outside this design.

The cache protects against *other local users* replacing a file the program
loads. It offers no defence against a process running as the same user, or
against an attacker who can already write that user's cache directory.

## Related

- The rest of the runtime: [Runtime]({{< ref "reference/runtime.md" >}}).
- How the cache and the extraction work: [Architecture]({{< ref "explanation/architecture.md" >}}#embedded-objects).
