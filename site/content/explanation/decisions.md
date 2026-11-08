---
title: "Design decisions"
description: "The decision records, the alternatives they rejected, and what is next."
weight: 40
---

The design is written down as decision records in the repository, one decision
per file, never edited after acceptance. This page is the map: what each record
decided, what it cost, and which parts of it are not in the build yet.

| Record | Status | Decides |
|---|---|---|
| [ADR-0001](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0001-header-first-bindings.md) | accepted, revised the same day | bind C++ headers, generate one shim thunk per construct the ABI cannot express |
| [ADR-0002](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0002-cppgo-built-shim.md) | accepted; **supersedes ADR-0001** | cppgo generates **and compiles** the shim itself; the user's build is untouched |
| [ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md) | accepted | `OpenFS`: embed the C++ side and extract it to a private cache |

The design contract behind them is
[`DESIGN.md`](https://github.com/Manu343726/cppgo/blob/main/DESIGN.md).
Where the contract and this site disagree about what *exists*, the code and its
tests win: the site describes the build in the tree.

## ADR-0001: headers are the source of truth

v1 and v2 were driven by Go declarations: `//cppgo:external` named a C symbol,
`//cppgo:class mathx::Vector` declared a class, and an `//cppgo:abi` directive
described what neither the parser nor the binary could. Two problems were real:
the Go declaration was the source of truth *and could lie*, and the API showed
the machinery (`type Vector cppgo.Handle`, `New`/`Destroy`, an ABI lecture on
every wrapper).

The decision: read the headers and emit a Go package plus a C++ translation unit
with one `extern "C"` thunk per binding the direct ABI cannot express. The first
version of the record made unsupported constructs a **build error**; the operator
rejected that, and the revised record makes completeness the product — a
generator that answers idiomatic C++ with "change your C++" falls short.

What it rules out: the Go declaration as the source of truth, and the hand-written
`extern "C"` layer.

## ADR-0002: cppgo builds its own shim

ADR-0001 left the shim in the **user's** build: cppgo wrote
`zz_cppgo_shim.cpp`, and the user's build compiled it. Three things then became
true together:

1. the constraint that everything is generated on the Go side, and the user's
   library is their own build, untouched by cppgo;
2. the clarification that this concerns the *user's build* — cppgo may generate
   and compile a shim of its own that links the user's library;
3. the target libraries, above all Eigen, which is header-only and template-heavy:
   the constructs that matter are exactly the ones a user's build should not have
   to compile.

The decision: cppgo emits and compiles `libcppgo_<package>.so`, linked against
the user's library, and the generated Go package loads the shim. Everything else
from ADR-0001 survives, with the carrier rules now measured.

The costs, in the record and on this site:

- cppgo needs a C++ compiler that matches the library's family, standard library
  and `_GLIBCXX_USE_CXX11_ABI`;
- two objects ship instead of one, and their load order matters;
- a callback-capable binding is about ten times the cost of a direct one, because
  it enters through `runtime.cgocall`;
- determinism now spans a compiler: the generated C++ is byte-identical, and the
  compiled shim is verified by compiling it, never by comparing binaries.

What it rules out: a generated file as a dependency of a build cppgo does not
own, and the "direct ABI only, refuse the rest" posture.

## ADR-0003: embed the C++ side

Deploying a cppgo program used to mean shipping the binary **and** the `.so`
beside it, or arranging `LD_LIBRARY_PATH`. ADR-0002 made that two objects. The
failure mode is a deployment bug: the build succeeds, the program starts, and the
first binding fails at the customer with a message about a file nobody shipped.

The decision: `cppgo.OpenFS(fsys, name)` materialises every `.so` in `fsys` in
`os.UserCacheDir()/cppgo/<sha256>/`, then loads one from there. The cache is
`0700`/`0600`, writes are atomic renames, and an entry is reused only when its
size and digest match. Siblings are found through `-Wl,-rpath,$ORIGIN`.

The rejected alternatives are worth keeping in mind, because each one is the
obvious idea:

- **`memfd_create` and `dlopen("/proc/self/fd/N")`** — Linux only, and the
  decisive point: such an object's `$ORIGIN` is `/proc/self/fd`, so the second
  object cannot be found at all;
- **a fixed name in `os.TempDir()`** — a predictable path in a shared,
  world-writable directory that another local user can pre-create;
- **a fresh temporary directory per run** — no reuse, and a crashed process
  leaves it behind forever;
- **one fat `.so`** — would remove the sibling problem, but the user's library is
  their own build and merging means re-linking somebody else's artefact;
- **static linking C++ into the Go binary** — loses the no-cgo property and the
  `dlopen` path the trampolines are measured on.

## What is next

Three things are decided or designed and **not shipped**. They are marked
wherever they appear in this documentation.

| Item | State | What a reader can rely on today |
|---|---|---|
| the full error projection ([issue #19](https://git.manu343726.com/manu343726/cppgo/issues/19)) | the projection is implemented and tested in `internal/errormap`; the generator does not consume it | exceptions and `-status` become Go errors; `std::error_code` and friends are refused at generation time |
| generics breadth, operators, inheritance | two-phase templates work for scalar type arguments and dimensions; operators, member templates, base classes and enums are refused | every refusal names the declaration and the fix |
| the OpenCV, ArrayFire and Eigen slices | designed as generated bindings tested against the real libraries, with build tags and a skip when a library is absent; nothing implemented | the two-phase flow the Eigen slice needs is in the tree and measured |
| embedding a *generated* package | `OpenFS` works; the generated `Open` does not call it yet | deploy the two objects, or use the runtime directly |

## Related

- The mechanisms the decisions chose: [Architecture]({{< ref "explanation/architecture.md" >}}).
- The measured limits: [Limits]({{< ref "explanation/limits.md" >}}).
- The records themselves, with their revision history:
  [docs/adr](https://github.com/Manu343726/cppgo/blob/main/docs/adr).
