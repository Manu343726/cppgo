# Decisions and status

The history of cppgo — a generator that writes a Go package from a C++ header,
plus the runtime that package calls — the decision records behind its design, the
alternatives they rejected, and the state of the work that is designed but not
shipped. Read it when you are about to change a mechanism and need to know why it
is the way it is, or when a design document and the code disagree about what
exists. It is developer material; the manual only states what exists today.

## The words this page uses

| Term | What it means here |
|---|---|
| **ADR** | architecture decision record: one file per decision, stating the forces, the choice and what it costs. [`docs/adr/`](../adr) holds them |
| **accepted** | the decision is in force. An accepted ADR is never edited |
| **superseded** | a later decision replaced it. The old record keeps its text and says which record replaced it |
| **designed, not shipped** | the mechanism is written down and may be prototyped, but no user can rely on it today |
| **`extern "C"`** | a C++ declaration with C linkage: the C calling convention and a name that is not mangled, so Go can find it in the object file |
| **mangled name** | the name a C++ compiler writes into the object for a function or variable, encoding the namespace, the parameter types and the class. `_ZN5mathx6Vector4normEv` is `mathx::Vector::norm()` |
| **carrier** | the route a binding takes across the boundary: the direct trampoline, a thunk in the shim, or the reentrant call |
| **thunk** | a generated C++ function that calls a declaration the trampoline cannot carry and returns a status instead |
| **shim** | the C++ library cppgo generates from the thunks and compiles itself, `libcppgo_<package>.so` |
| **reentrant** | the carrier that lets C++ call back into Go: it goes through the runtime's cgo callback entry, which the direct trampoline cannot provide |
| **goroutine, stack** | a goroutine is a Go function running with its own stack; the stack is where its frames live and may be copied when it grows |
| **`std::`** | the namespace of the C++ standard library: `std::string`, `std::vector`, `std::error_code` and the rest are its types |
| **`_GLIBCXX_USE_CXX11_ABI`** | the libstdc++ (the GNU C++ standard library) macro that chooses between its two incompatible `std::string` layouts. Two objects built with different values cannot pass a `std::string` to each other |
| **`$ORIGIN`** | the dynamic loader's token for "the directory the object was loaded from", used so a shared object can find a sibling without an absolute path |
| **`dlopen`, `dlsym`, `dlclose`** | the C library calls that open a shared object, look up a symbol in it and close it |
| **`memfd_create`** | a Linux call that creates an anonymous file in memory, which a program can pass to `dlopen` as `/proc/self/fd/N` |

## Decision records

One decision per file, never edited after acceptance:

| Record | Status | Decides |
|---|---|---|
| [ADR-0001: bind C++ headers](../adr/0001-header-first-bindings.md) | accepted, revised the same day | read C++ headers, and emit one shim thunk per construct the C ABI cannot express |
| [ADR-0002: cppgo builds its own shim](../adr/0002-cppgo-built-shim.md) | accepted; **supersedes ADR-0001** | cppgo generates **and compiles** the shim itself; the user's build is untouched |
| [ADR-0003: embed the C++ side](../adr/0003-embedded-libraries.md) | accepted | `OpenFS`: embed the C++ side and extract it to a private cache |

The design contract behind them is [`DESIGN.md`](../../DESIGN.md). Where the
contract and a page disagree about what *exists*, the code and its tests win; see
[Evidence](evidence.md) for the mapping.

## History

The four versions below are generations of the design, not releases: the project
has never tagged a release. Each replaced the previous one before it shipped.

**v1 and v2 were driven by Go declarations.** `//cppgo:external` named a C
symbol, `//cppgo:class mathx::Vector` declared a class, and an `//cppgo:abi`
directive described what neither the parser nor the binary could. Two problems
were real: the Go declaration was the source of truth *and could lie*, and the
API showed the machinery (`type Vector cppgo.Handle`, `New`/`Destroy`, an ABI
lecture on every wrapper).

**The first v3 design refused what the direct ABI could not express:** an inline
body, a virtual call, a `std::` type or a by-value non-POD was a build error. The
operator rejected that posture, and the revised record makes completeness the
product — a generator that answers idiomatic C++ with "change your C++" falls
short, because the constructs being refused are exactly what real headers are
made of.

**The v4 design reversed one thing:** the shim left the user's build. It is now
generated and compiled by cppgo, into an object of its own that links the user's
library (ADR-0002, superseding ADR-0001).

## ADR-0001: headers are the source of truth

The decision: read the headers and emit a Go package plus a C++ translation unit
with one `extern "C"` thunk per binding the direct ABI cannot express.

What it rules out: the Go declaration as the source of truth, and the
hand-written `extern "C"` layer.

A binding gets exactly one carrier. **Direct** is the trampoline that calls the
mangled C++ symbol, reserved for `noexcept`, non-virtual, out-of-line functions
whose arguments fit the register rules. **Thunk** is generated C++ that calls the
target as C++: inline bodies, virtual methods, template instantiations, `std::`
types, by-value non-PODs, `this` adjustment, exception catching, and argument
blocks when the registers run out.

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
- a callback-capable binding costs far more than a direct call: the record priced
  the callback at ~10× the direct call (151 ns against 15.9 ns), because the
  argument block cannot live on a goroutine stack a callback may grow. The
  current suite measures the generated callback at 5.5 times cgo's — 499.2 ns
  against 90.4 ns — because it registers the callback per call; see
  [Benchmarks](benchmarks.md). The record's own figure was an earlier run and
  the suite is the oracle now;
- determinism now spans a compiler: the generated C++ is byte-identical, and the
  compiled shim is verified by compiling it, never by comparing binaries.

What it rules out: a generated file as a dependency of a build cppgo does not
own, and the "direct ABI only, refuse the rest" posture.

## ADR-0003: embed the C++ side

Deploying a cppgo program used to mean shipping the binary **and** the `.so`
beside it, or arranging `LD_LIBRARY_PATH` — the environment variable that adds
directories to the loader's search path. ADR-0002 made that two objects. The
failure mode is a deployment bug: the build succeeds, the program starts, and the
first binding fails at the customer with a message about a file nobody shipped.

The decision: `cppgo.OpenFS(fsys, name)` takes `fsys`, an `embed.FS` (the Go
value that carries files inside the binary), materialises every `.so` in it under
`os.UserCacheDir()/cppgo/<sha256>/` — the per-user cache directory the operating
system reports — then loads one from there. The directory name is a SHA-256 over
the set's file names, sizes and contents, so two versions of the same object
cannot collide.

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
| the full error projection: `std::error_code` (a library's own error value), `std::expected` (a value or an error), `errno` (C's per-thread error number), `std::variant` (one of several types), configured out-parameters and custom result types | the projection is implemented and tested in [`internal/errormap`](../../internal/errormap); the generator does not consume it | [#19](https://git.manu343726.com/manu343726/cppgo/issues/19) |
| generics breadth | two-phase templates work for scalar type arguments and dimensions; a public field on a class template, a function template whose signature names a dependent type, and a parameter pack are not part of the generic API | the wider generator work, under epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1), an issue that groups the work |
| the OpenCV (computer vision), ArrayFire (GPU arrays) and Eigen (header-only linear algebra) slices | shipped, phase 1: [`examples/opencv`](../../examples/opencv), [`examples/arrayfire`](../../examples/arrayfire) and [`examples/eigen`](../../examples/eigen) each bind the declarations the generator carries and test them against the installed library; `cv::Mat`, `af::array` and Eigen's matrices wait on [#112](https://git.manu343726.com/manu343726/cppgo/issues/112), and by-value records since [#102](https://git.manu343726.com/manu343726/cppgo/issues/102) already let a C++ free function hand Go a `cv::Mat` or an `af::array` | [#42](https://git.manu343726.com/manu343726/cppgo/issues/42), epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1) |
| embedding a *generated* package | `OpenFS` works; the generated `Open` does not call it yet | [ADR-0003](../adr/0003-embedded-libraries.md) |
| a user-facing benchmark comparison | shipped: the suite in [`benchmarks/`](../../benchmarks) measures every arm and the manual publishes its medians | the fixes its findings asked for are tracked in [#51](https://git.manu343726.com/manu343726/cppgo/issues/51) |

## Known defects

- **A `(char *, size_t)` parameter binds as `[]int8`, not as the `string`
  [`DESIGN.md`](../../DESIGN.md) specifies.** The binding compiles and the shim
  builds; the type map and the generator disagree. The measurement is in
  [Evidence](evidence.md).

[Limits](../../site/content/explanation/limits.md) § *`char *` parameters* now
states both shapes: an unpaired `const char *` crosses as a `string`, and the
pair as a borrowed `[]int8`. The pair is the defect the type map still describes
differently.
