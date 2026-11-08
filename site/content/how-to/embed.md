---
title: "Embed the C++ side in the Go binary"
description: "OpenFS and go:embed: what works today, and what the generated Open does not do yet."
weight: 40
---

Normally a program that uses a binding ships its binary and two shared objects,
and the pair has to stay together. `cppgo.OpenFS` removes the second half of
that: it takes a file system of embedded bytes, writes the objects to a private
cache, and loads one of them by name. The C++ side then travels inside the Go
binary.

**Status.** `OpenFS` is implemented and measured in the runtime. The package
`cppgo gen` writes **does not call it yet**: its `Open` loads the two objects
from the filesystem, and the wiring of `OpenFS` into generated packages is
recorded as emitter work in
[ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md).
So today this page is for code that uses the runtime directly; a generated
binding still deploys as described in
[Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).

## The call

```go
func OpenFS(fsys fs.FS, name string) (*Library, error)
```

`fsys` is any `fs.FS`: an `embed.FS` from `//go:embed` in production, and
`fstest.MapFS` or `os.DirFS` in tests. `name` is the base name of the object to
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
	// ... call it with cppgo.CallReentrant or a hand-written trampoline
}
```

Every `.so` in `fsys` is extracted, not just `name`. That is not an
optimisation: the shim's `DT_NEEDED` entry names your library, and the loader can
only find it if both are in one directory. Entries are flattened into a single
directory (base names kept), because that is the only layout in which
`$ORIGIN` sees a sibling. Two entries with the same base name are an error, and
other files are ignored.

The shim must be linked with `-Wl,-rpath,$ORIGIN` for the sibling to be found.
cppgo passes that flag when it builds the shim; a user library that has its own
siblings has to pass it too.

## Where the bytes go

```
os.UserCacheDir()/cppgo/<sha256 of the whole set>/<file name>
```

The digest covers every object of the set — name, size and SHA-256 of the
contents — in file-name order. Two programs embedding the same bytes share one
entry; a change to any byte of any object lands in a new directory, so a stale
entry is never mixed with new contents.

- the cache root and every entry directory are `0700`; every object is `0600`.
  A mode something else loosened is tightened on the next call;
- an object is written to a temporary file **in the same directory** (`O_EXCL`,
  `0600`, randomised name) and renamed into place, so no reader sees a partial
  object;
- an existing file is reused only when its size and SHA-256 both match. A
  truncated or corrupted entry is replaced;
- a path that is not a private directory, or a symbolic link where an object
  should be, is refused with `cppgo.ErrInsecureCache` instead of being used.
  Those are shapes another local user could have prepared in advance.

Measured on the real `embed.FS` path, with a demo program built
`CGO_ENABLED=0` (ADR-0003):

```console
$ XDG_CACHE_HOME=/tmp/…/demo-cache ./demo
loaded /tmp/…/demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
cppgo_embed_main_add(2, 3) = 10
$ stat -c '%A %n' demo-cache/cppgo demo-cache/cppgo/*
drwx------ demo-cache/cppgo
drwx------ demo-cache/cppgo/ae667028…924bf1
-rw------- demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_dep.so
-rw------- demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
```

## Escape hatches

| What | Effect |
|---|---|
| `CPPGO_LIBRARY_PATH=<path>` | `OpenFS` loads that path and extracts nothing. It is the development override: point the same program at a library the C++ build left in the tree |
| `cppgo.CacheDir()` | names the cache (`os.UserCacheDir()/cppgo`); a function, because the user cache directory reads the environment |
| `cppgo.CleanCache()` | removes every extracted entry. Clearing the cache costs one extraction, never correctness |

## What embedding does not buy

The Go binary is still dynamically linked to the C library: built with
`CGO_ENABLED=0`, the demo is `dynamically linked, interpreter
/lib64/ld-linux-x86-64.so.2`, because the loader purego uses needs libc. The
embedded C++ objects still declare `libstdc++.so.6`, `libm.so.6`,
`libgcc_s.so.1` and `libc.so.6` as needed — `$ORIGIN` changes where *siblings*
are looked up, not where the platform's runtimes are. A program that must run
where libstdc++ is absent needs a static build of the C++ side instead, which is
outside this design.

The cache protects against *other local users* replacing a library the program
maps. It offers no defence against a process running as the same user, or
against an attacker who can already write that user's cache directory.

## Related

- The decision and its alternatives: [ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md).
- The rest of the runtime: [Runtime]({{< ref "reference/runtime.md" >}}).
