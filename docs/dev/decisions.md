# Decisions and status

The project's history, the decision records behind the design, the alternatives
they rejected, and the state of the work that is designed but not shipped. This is
developer material; the manual only states what exists.

## Decision records

One decision per file, never edited after acceptance:

| Record | Status | Decides |
|---|---|---|
| [ADR-0001](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0001-header-first-bindings.md) | accepted, revised the same day | bind C++ headers, generate one shim thunk per construct the ABI cannot express |
| [ADR-0002](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0002-cppgo-built-shim.md) | accepted; **supersedes ADR-0001** | cppgo generates **and compiles** the shim itself; the user's build is untouched |
| [ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md) | accepted | `OpenFS`: embed the C++ side and extract it to a private cache |

The design contract behind them is
[`DESIGN.md`](https://github.com/Manu343726/cppgo/blob/main/DESIGN.md). Where the
contract and the manual disagree about what *exists*, the code and its tests win.

## History

v1 and v2 were driven by Go declarations: `//cppgo:external` named a C symbol,
`//cppgo:class mathx::Vector` declared a class, and an `//cppgo:abi` directive
described what neither the parser nor the binary could. Two problems were real:
the Go declaration was the source of truth *and could lie*, and the API showed
the machinery (`type Vector cppgo.Handle`, `New`/`Destroy`, an ABI lecture on
every wrapper).

The first v3 design refused constructs the direct ABI could not express: an
inline body, a virtual call, a `std::` type or a by-value non-POD was a build
error. The operator rejected that posture, and the revised record makes
completeness the product — a generator that answers idiomatic C++ with "change
your C++" falls short, because the constructs being refused are exactly what real
headers are made of.

The v4 design reversed one thing: the shim left the user's build. It is now
generated and compiled by cppgo, into an object of its own that links the user's
library (ADR-0002, superseding ADR-0001).

## ADR-0001: headers are the source of truth

The decision: read the headers and emit a Go package plus a C++ translation unit
with one `extern "C"` thunk per binding the direct ABI cannot express.

What it rules out: the Go declaration as the source of truth, and the
hand-written `extern "C"` layer.

## ADR-0002: cppgo builds its own shim

ADR-0001 left the shim in the **user's** build: cppgo wrote `zz_cppgo_shim.cpp`,
and the user's build compiled it. Three things then became true together:

1. the constraint that everything is generated on the Go side, and the user's
   library is their own build, untouched by cppgo;
2. the clarification that this concerns the *user's build* — cppgo may generate
   and compile a shim of its own that links the user's library;
3. the target libraries, above all Eigen, which is header-only and
   template-heavy: the constructs that matter are exactly the ones a user's build
   should not have to compile.

The decision: cppgo emits and compiles `libcppgo_<package>.so`, linked against
the user's library, and the generated Go package loads the shim.

The costs, in the record:

- cppgo needs a C++ compiler that matches the library's family, standard library
  and `_GLIBCXX_USE_CXX11_ABI`;
- two objects ship instead of one, and their load order matters;
- a callback-capable binding costs far more than a direct call: the record's ~10×
  priced the trampoline alone, and the suite measures the generated binding at
  ~43× (1776 ns against 40.9 ns) because it registers the callback per call —
  see [Benchmarks](benchmarks.md);
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
`os.UserCacheDir()/cppgo/<sha256>/`, then loads one from there.

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

## Designed and not shipped

| Item | State | Tracked by |
|---|---|---|
| the full error projection (`std::error_code`, `std::expected`, `errno`, `std::variant`, configured out-parameters, custom result types) | the projection is implemented and tested in `internal/errormap`; the generator does not consume it | [#19](https://git.manu343726.com/manu343726/cppgo/issues/19) |
| generics breadth, operators, inheritance, enums | two-phase templates work for scalar type arguments and dimensions; the rest are refused with a message | the wider generator work, under epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1) |
| the OpenCV, ArrayFire and Eigen slices | designed as generated bindings tested against the real libraries, with build tags and a skip when a library is absent; nothing implemented | epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1) |
| embedding a *generated* package | `OpenFS` works; the generated `Open` does not call it yet | [ADR-0003](https://github.com/Manu343726/cppgo/blob/main/docs/adr/0003-embedded-libraries.md) |
| a user-facing benchmark comparison | shipped: the suite in `benchmarks/` measures every arm and the manual publishes its medians | the fixes its findings asked for are tracked in [#51](https://git.manu343726.com/manu343726/cppgo/issues/51) |

## Known defects

- **`char *` / `const char *` parameters do not bind.** The type maps to `*int8`
  and the generated C++ fails to compile. `DESIGN.md`'s type map says
  `(char *p, size_t n)` becomes a Go `string`. The measurement and the message
  are in [Evidence](evidence.md#char--parameters-do-not-bind).
