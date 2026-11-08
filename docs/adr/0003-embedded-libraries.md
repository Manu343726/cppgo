# ADR-0003: Ship the C++ side inside the Go binary, extracted to a private cache

- Status: accepted
- Date: 2026-11-08
- Deciders: operator
- Relates to: [ADR-0002](0002-cppgo-built-shim.md) (the shim is a second object)

## Context

A cppgo program loads its C++ side at run time: `cppgo.Open(path)` asks the dynamic loader for a
shared object. Deploying one therefore used to mean deploying the Go binary **and** the `.so` beside
it — or arranging `LD_LIBRARY_PATH`, or a loader configuration file. ADR-0002 made that worse in one
specific way: the cppgo shim is now a second object that cppgo compiles itself and links against the
user's library, so there are two files whose relative position is load-bearing.

The failure mode is a deployment bug rather than a compile bug: the build succeeds, the program
starts, and the first binding fails at the customer, in the loader, with a message about a file
nobody shipped. The product claim this ADR has to make true is *one file to ship for the C++ side*.

`//go:embed` already puts arbitrary bytes in the Go binary. What it does not do is put them on a
path: glibc's `dlopen` takes a **path**, and there is no portable way to hand it a memory buffer.
Something has to write the bytes out. The only question is where, with what permissions, and how a
second object finds the first.

## Decision

The runtime gains one function, next to `Open`:

```go
func OpenFS(fsys fs.FS, name string) (*Library, error)
```

It materialises **every** `.so` in `fsys` in a private per-user cache, then `dlopen`s `name` from
there. `fsys` is any `fs.FS`: an `embed.FS` from `//go:embed` in production, `fstest.MapFS` or
`os.DirFS` in tests and during development. The mechanism below is the contract.

### Cache layout and naming

```
os.UserCacheDir()/cppgo/<sha256 of the whole set>/<file name>
```

The digest covers every object of the set — its file name, its size and the SHA-256 of its contents,
in file-name order — so it is deterministic, two programs embedding the same bytes share one entry,
and a change to any byte of any object lands in a new directory. A stale entry is never mixed with
new contents; it is simply not looked at again (and `CleanCache` removes it).

Files keep the embedded names and are flattened into that one directory, including entries that sit
in a subdirectory of the file system: one flat directory is the only layout in which a sibling can
be found by the mechanism in the next paragraph. Two entries with the same base name are an error,
not a silent last-one-wins, and files that are not `.so` are ignored.

### `$ORIGIN`: how the sibling is found

The dynamic loader resolves an object's `DT_NEEDED` entries through *that object's* `DT_RUNPATH`
(and `DT_RPATH`), and `$ORIGIN` in a run path means "the directory the object itself was loaded
from". So the object must be linked

```
-Wl,-rpath,$ORIGIN
```

Measured on the fixture pair (main calls the sibling, both objects in the cache):

```
$ readelf -d libcppgo_embed_main.so | grep -E 'NEEDED|RUNPATH'
 0x0000000000000001 (NEEDED)  Shared library: [libcppgo_embed_dep.so]
 0x0000000000000001 (NEEDED)  Shared library: [libstdc++.so.6]
 0x0000000000000001 (NEEDED)  Shared library: [libm.so.6]
 0x0000000000000001 (NEEDED)  Shared library: [libgcc_s.so.1]
 0x0000000000000001 (NEEDED)  Shared library: [libc.so.6]
 0x000000000000001d (RUNPATH) Library runpath: [$ORIGIN]
```

and the same link without the flag fails exactly at the sibling:

```
$ dlopen libcppgo_embed_main.so   # linked without -Wl,-rpath,$ORIGIN
libcppgo_embed_dep.so: cannot open shared object file: No such file or directory
```

That is why the test fixture is linked with the flag and why the test reads `DT_RUNPATH`/`DT_NEEDED`
back out of the ELF instead of trusting the build: dropping the flag must fail the test, not fail
somebody's deployment. The cppgo shim build (ADR-0002) is the caller that must pass it, and so is
any user library that itself has siblings.

### Permissions, atomicity and concurrency

- the cache root and every entry directory are `0700`, every object is `0600`; a mode that time or
  another tool loosened is tightened on the next call;
- an object is written to `os.CreateTemp(dir, …)` in **that same directory** (mode `0600`, `O_EXCL`,
  randomised name) and renamed into place, so no reader ever sees a partial object and no temporary
  path is predictable or world-writable;
- an existing file is reused only when its size **and** SHA-256 match the embedded bytes. A truncated
  or corrupted entry is replaced; a symbolic link is never followed and is replaced by the rename that
  follows; a directory at an object path cannot be replaced at all, and the rename that tries fails
  with an `*ExtractError` naming the object;
- a path where an entry directory belongs that is not a private directory — a symbolic link, which
  `MkdirAll` follows and `Lstat` does not, a regular file in its place, or a directory whose mode
  cannot be tightened — is refused with `ErrInsecureCache` rather than used, because those are the
  shapes another local user could have prepared in advance;
- extraction happens in full before `dlopen`, in every process, so a concurrent process cannot load a
  half-extracted set: it either finds every file with the right digest or writes the missing ones
  itself. The rename is atomic per file, and two writers of the same bytes produce the same bytes.

The set is hashed on **every** call. That is one read of the embedded bytes, and it is what makes
"reuse only after the digest matches" a property instead of a hope: a cache entry is never trusted
because of its name alone.

### Override and cleanup

- `CPPGO_LIBRARY_PATH` (exported as `cppgo.LibraryPathEnv`) makes `OpenFS` load that explicit path and
  extract nothing. It is the development escape hatch: the same program can be pointed at a library
  the C++ build left in the tree.
- `cppgo.CacheDir()` names the cache and `cppgo.CleanCache()` removes it. The cache is derived state,
  so clearing it costs one extraction, never correctness.

## Alternatives considered

- **`memfd_create` and `dlopen("/proc/self/fd/N")`** — rejected, and not only for portability. It is
  Linux-only (purego's loader also serves darwin), and — the decisive point — `$ORIGIN` of such an
  object is `/proc/self/fd`, which is not a directory of siblings, so the second object ADR-0002
  introduced could not be found at all. An anonymous inode cannot be the neighbour of another
  anonymous inode. Nothing in this feature needs to avoid the disk, so the memory route buys nothing
  and loses the dependency story.
- **Extract to `os.TempDir()` under a fixed name** — a predictable path in a shared, world-writable
  directory: another local user can pre-create it, replace the object between write and `dlopen`, or
  plant a symlink. The user cache directory with `0700`/`0600` and `O_EXCL` closes that.
- **Extract to a fresh `MkdirTemp` on every run** — no reuse (a full write of the embedded set per
  start), and no owner: a crashed or killed process leaves the directory behind forever. Content
  addressing gives reuse and a natural key for cleanup.
- **A single fat `.so`** (merge the shim and the user's library) — would remove the sibling problem,
  but the user's library is their own build and ADR-0002 keeps it untouched; merging means re-linking
  or rewriting somebody else's artefact.
- **Link the C++ statically into the Go binary** — not available to this design: cppgo is cgo-free by
  contract, and a C++ archive in a Go binary would go through cgo and lose the `dlopen` path the
  trampolines are measured on.
- **`dlopen` from a memory buffer** — not a portable glibc interface; see `memfd_create` above.

## Consequences

**The claim.** For the C++ side, the Go binary is the only file to ship: `//go:embed` the objects,
call `OpenFS`, and the first run writes them out and loads them. Nothing has to be installed next to
the program and nothing has to be put on the loader's search path.

**The limit.** It is not a static binary. Built with `CGO_ENABLED=0`, the demo program is still
`dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2`, and `ldd` lists `libc.so.6`,
`libdl.so.2`, `libpthread.so.0` — purego's loader needs the system C library to exist. The embedded
objects themselves still declare `libstdc++.so.6`, `libm.so.6`, `libgcc_s.so.1` and `libc.so.6` as
needed: `$ORIGIN` changes where *siblings* are looked up, not whether the platform's C and C++
runtimes exist. `OpenFS` embeds what cppgo knows about; a system dependency is still a system
dependency, and a program that must run where libstdc++ is absent has to ship a static build of the
C++ side instead.

**Costs, stated rather than hidden.** The first run writes the embedded set once per user and per
content digest. Every run reads it to verify the digest. The cache lives under
`os.UserCacheDir()` until `CleanCache`, so an upgrade leaves the old entry until it is cleared. A
bound program that is *not* embedded still uses `Open` and still needs its library path; `OpenFS` is
an addition, not a replacement.

**What is not a security boundary.** The cache protects against *other local users* replacing a
library the program maps. It is not a defence against a process running as the same user, and not a
defence against an attacker who can already write the user's cache directory.

**Wiring left to the emitter.** A generated package's `Open` calling
`cppgo.OpenFS(<package>Files, "libcppgo_shim.so")` with the package's own `//go:embed` is emitter
work of the v4 emitter, not part of this prototype.

## Evidence

Measured in this tree with `make test` (two passes: `CGO_ENABLED=0`, then `CGO_ENABLED=1 -race`):

- the fixture pair (`libcppgo_embed_dep.so`, `libcppgo_embed_main.so` linked `-Wl,-rpath,$ORIGIN`) is
  served through `fstest.MapFS` and `os.DirFS`, extracted, loaded and called from Go; the same test
  asserts the ELF's `DT_RUNPATH` contains `$ORIGIN` and `DT_NEEDED` names the sibling;
- 32 goroutines and 4 child processes extract the same set at once: one cache entry, every load
  returns the sibling-dependent result;
- a second call reuses the files (their mtimes are unchanged); a same-size corruption and a truncated
  sibling are both replaced, and a correct file with a loose mode is tightened without being
  rewritten;
- a missing entry (`fs.ErrNotExist` before anything is written), an unusable cache, a non-private
  cache path, a walk/open/read/copy failure, a file that changes between hashing and copying, and a
  file that is not a loadable ELF each produce a typed error naming the object and the cache;
- the real `embed.FS` path, on a separate program built with `CGO_ENABLED=0`:

```
$ XDG_CACHE_HOME=/tmp/…/demo-cache ./demo
loaded /tmp/…/demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
cppgo_embed_main_add(2, 3) = 10
$ find demo-cache -mindepth 1 | sort
demo-cache/cppgo
demo-cache/cppgo/ae667028…924bf1
demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_dep.so
demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
$ stat -c '%A %n' demo-cache/cppgo demo-cache/cppgo/* demo-cache/cppgo/*/*
drwx------ demo-cache/cppgo
drwx------ demo-cache/cppgo/ae667028…924bf1
-rw------- demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_dep.so
-rw------- demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
```

Eight concurrent runs of that binary all printed the same path and `= 10`, and `ls demo-cache/cppgo`
held one entry afterwards.
