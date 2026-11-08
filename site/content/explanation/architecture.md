---
title: "Architecture"
description: "How cppgo works: the artefacts it produces, and one call from Go to C++."
weight: 20
---

This is the how-it-works page. Everything a user needs in order to *use* cppgo is
on the other pages — [Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}})
to start, the [how-to guides]({{< ref "how-to/_index.md" >}}) for a task,
[Reference]({{< ref "reference/_index.md" >}}) to look something up. This page
explains the machinery behind them, and the passages that used to sit in the
user pages live here now.

cppgo is two products that ship together: a **runtime** that Go programs link,
and a **generator** that writes and compiles the C++ side. Everything else in
this page follows from that split and from the three artefacts one generation
produces.

## Two layers

| Layer | Package | Owns | Depends on |
|---|---|---|---|
| runtime | `github.com/Manu343726/cppgo` (repository root) | dynamic loading, symbol resolution, the C ABI trampolines, the reentrant carrier, stack probing, foreign-object storage, wire helpers, error types, the callback table | the standard library, `internal/asm`, `internal/rtl`, `internal/callback`, `purego` |
| generator | `internal/gen`, `internal/cxx`, `cmd/cppgo` | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim, and compiling the shim | anything, including libclang and a C++ compiler |

The runtime **never imports `internal/cxx`**, directly or transitively, and has
no libclang dependency. That is a test, not a convention:
`TestRuntimeHasNoGeneratorDependency` in the root package runs `go list -deps`
and fails when `internal/cxx`, `cmd/cppgo` or `internal/gen` appears in the
runtime's dependency graph. A program built with a generated package needs no
libclang and no C++ compiler; it needs the two shared objects at run time.

## Three artefacts

| Artefact | Owner | What it is |
|---|---|---|
| the user's library | you | your sources, your compiler flags, your build. cppgo never writes into it |
| the cppgo shim | cppgo | `zz_cppgo_shim.cpp`, generated from the same parse as the Go API and compiled by cppgo into `libcppgo_<package>.so`, linked against your library |
| the generated Go package | cppgo | `zz_cppgo.gen.go` and `zz_cppgo_amd64.s`: the API you call, one trampoline per binding, and the code that loads the shim |

The dotted line is the contract's edge: **your build is untouched, and no
generated code is in it.**

## The generated files

```
your header + your compilation configuration
        │
        ▼
   cppgo gen ──► zz_cppgo.gen.go   (the Go API)
        │        zz_cppgo_amd64.s  (one trampoline per binding)
        │        cppgo_shim/*.cpp  (the thunks)
        │        libcppgo_<pkg>.so (the shim, compiled by cppgo)
        ▼
   your Go program
```

| File | Contents |
|---|---|
| `zz_cppgo.gen.go` | the Go API, a load function, and one declaration per trampoline |
| `zz_cppgo_amd64.s` | one NOSPLIT assembly trampoline per binding: it aligns the stack, makes the call, restores it |
| `cppgo_shim/zz_cppgo_shim.h` | the `extern "C"` prototypes of the thunks |
| `cppgo_shim/zz_cppgo_shim.cpp` | the thunks themselves, one per non-direct binding |
| `libcppgo_<package>.so` | the shim, compiled by cppgo and linked against your library |

The first four are sources, and the examples commit them because an example is
teaching material and the command in the file banner reproduces them. The `.so`
is a build artefact. Nothing in this list is written into your library.

*NOSPLIT* is a Go assembly attribute: it tells the runtime not to grow the
goroutine's stack at that point, which is what makes it legal to switch to a
16-byte-aligned stack for the C call.

## One call, step by step

Take `mathx::Vector::at(std::size_t) const`, generated as `At`, carrier
**thunk** because it may throw.

1. **Go calls `At(2)`.** The generated method checks the receiver, calls
   `cppgo.ProbeStack()`, and forms a pointer to its result local.
2. **The trampoline runs.** `cppgoCall_79aeb346bd10` is a NOSPLIT assembly
   function with no Go frame:

   ```asm
   TEXT ·cppgoCall_79aeb346bd10(SB), NOSPLIT, $0-36
   	MOVQ	fn+0(FP), R12
   	MOVQ	this+8(FP), DI
   	MOVQ	a1+16(FP), SI
   	MOVQ	out+24(FP), DX
   	MOVQ	SP, R13
   	ANDQ	$~15, SP
   	CALL	R12
   	MOVQ	R13, SP
   	MOVL	AX, ret+32(FP)
   	RET
   ```

   It reads its arguments before switching the stack pointer, aligns `SP` to 16
   bytes for the System V convention, calls, restores `SP`, and only then stores
   the result. `ProbeStack` ran first because the probe grows the goroutine
   stack, and growing it after taking a pointer into the caller's frame would
   move that frame.
3. **The thunk runs as C++.** The address in `R12` is `cppgo_thunk_79aeb346bd10`
   in the shim, which clears its thread-local message, casts the `this` pointer,
   and calls the real method inside `try`/`catch`.
4. **The result comes back through Go storage.** The thunk writes the `double`
   through the out-pointer the caller provided, and returns `0` or `1`.
5. **The generated method maps the status.** Non-zero becomes a
   `*cppgo.CallError` reading the shim's message; zero returns the value.

A **direct** binding is the same path with step 3 replaced by the C++ symbol
itself and no status: `scale` is `noexcept`, so the call goes straight to
`_ZN5mathx6Vector5scaleEd` and there is nothing to map.

A **reentrant** binding differs at step 2: `cppgo.CallReentrant` fills an
argument block and enters through `runtime.cgocall`, so the thread is inside a
foreign call and a callback can re-enter Go.

In the `mathx` example, `scale` is the one direct binding: it is `noexcept`, not
virtual, defined out of line, and the library exports its mangled name. `x()` is
a thunk even though it is a trivial accessor, because its body is inline and the
library exports no symbol for it. The rest can throw or must be copied, so the
shim carries them. The
[carrier table]({{< ref "explanation/carriers.md" >}}) has the full matrix and
the costs.

## Exceptions across the boundary

A declaration that is not `noexcept` is carried by a thunk, and the thunk wraps
the call in `try`/`catch` — the call *and* the marshalling, because constructing
a `std::string` or a `std::vector` can throw `std::bad_alloc` too. This is the
generated code for `mathx::Vector::norm`
([`examples/mathx/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp)):

```cpp
extern "C" int32_t cppgo_thunk_49993cda6560(const void * self, double * cppgo_result) {
	cppgo_error_message.clear();
	try {
		*cppgo_result = static_cast<const ::mathx::Vector *>(self)->norm();
		return 0;
	} catch (const std::exception &error) {
		cppgo_error_message = error.what();
		return 1;
	} catch (...) {
		cppgo_error_message = "unknown C++ exception";
		return 1;
	}
}
```

The mechanism in four sentences: each thunk clears a thread-local message on
entry; a caught exception copies `what()` into it; the shim exports it through
`cppgo_last_error`; the generated binding reads it after a non-zero status and
returns it.

**No exception can be supported without a C++ frame above the call.** A direct
binding is `noexcept` by construction, and a C++ exception thrown with no C++
frame above it terminates the process. That is measured three times in
`internal/callback`, from a direct call, after a callback returned, and through a
Go callback frame. It is why a throwing function is never assigned the direct
carrier, even when its signature would fit the registers.

The practical consequence for a user is on
[Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}): an API that
throws and whose headers the shim cannot include cannot be bound safely, and the
fix belongs in the library.

## Values, copies, and the layout check

A record is **mirrorable** when all its fields are public, its layout is the
natural Go layout, and no field is a bitfield, a reference, a pointer, a union
member or a non-mirrorable type. Its Go value *is* the C layout, and the
generated file asserts that at compile time:

```go
// The C++ layout is proven at compile time: a wrong size, alignment or
// field offset does not compile.
var _ [16]byte = [unsafe.Sizeof(Point{})]byte{}
var _ [8]byte = [unsafe.Alignof(Point{})]byte{}
var _ [0]byte = [unsafe.Offsetof(Point{}.X)]byte{}
var _ [8]byte = [unsafe.Offsetof(Point{}.Y)]byte{}
```

A record that is not mirrorable becomes an opaque value instead: Go storage the
C++ side owns, with `Close`, `Clone` and generated accessors.

By-value records are copied by the shim in both directions. `mathx::midpoint`,
for example, takes a 16-byte POD by value and returns one: the thunk copies the
arguments into the call and the result back into Go storage.

## The `std::` ABI

`std::string`, `std::vector` and friends never appear in Go. Their layout exists
only in the shim, which is C++ and can legally construct them:

- a `std::string` parameter is copied into a `std::string` in the shim, and a
  result is copied out into a Go string;
- a `std::vector<T>` is copied element by element in both directions;
- `std::string_view` and `std::span` are borrowed as `(pointer, length)` and are
  valid only for the call;
- the shim and your library must use the same standard library and the same
  `_GLIBCXX_USE_CXX11_ABI`. cppgo compiles the shim with the flags it
  discovered and reports the compiler it used; it does not inspect the
  library's standard library, so keeping the two in step is a build decision.
  Recording the version and `_GLIBCXX_USE_CXX11_ABI` and refusing a mismatch is
  designed ([Limits]({{< ref "explanation/limits.md" >}})).

## Templates: why two phases

A C++ template does not exist until it is instantiated. `std::vector<int>` and
`std::vector<double>` are two different types with two different pieces of
machine code, and which ones your program needs is a property of the *Go*
program, not of the header.

That is why templates take two phases. `cppgo gen` writes the generic Go API,
which compiles without any concrete type. `cppgo build -scan <package>` loads the
program with `go/packages`, reads the type arguments at every use of the generic
API, and for each specialisation writes the explicit instantiation into the
shim's source:

```cpp
template struct ::box::Box<int>;
```

plus one thunk per member the program uses, and then compiles the shim. A Go
instantiation with no C++ counterpart does not reach a nil call: it fails when
it is used, and the error names the line to add.

The user-facing steps are in
[Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## Generation: parse, selection, determinism

**The parse.** `cppgo gen` reads the headers through libclang, which only sees
the program the compiler would see when it is given the same include paths and
language standard. The configuration comes from `compile_commands.json` or
`.clangd`. cppgo rebases a database's relative include paths against the entry's
own `directory`, because libclang resolves a relative path against *cppgo's*
working directory, which is a different tree: the parse then succeeds while
seeing a different program. `TestRebaseArgs` in `internal/gen` pins the rebase.

**The libclang floor.** The generator needs libclang 20 or newer. It reads the
offset of base-class subobjects through `clang_getOffsetOfBase`, which libclang
19 does not export, so a tree built against 19 fails at load time with
`undefined symbol: clang_getOffsetOfBase`. The CI image pins clang 22
(`ARG CLANG_VERSION=22` in the
[Dockerfile](https://github.com/Manu343726/cppgo/blob/main/Dockerfile)).

**Selection.** A header is the unit of selection: every public declaration in it,
plus the type closure of every bound signature. `-bind` and `-exclude` narrow
the set, and an entry that names nothing is an error. The single list of
constructs this build does not emit is `rejectUnimplemented` in
`internal/gen/reject.go`; each entry names the construct and the fix, which is
what [Limits]({{< ref "explanation/limits.md" >}}) tabulates.

**Determinism.** The same inputs produce byte-identical Go and C++. Bindings are
sorted by qualified name, a thunk's name is a stable hash of the qualified
signature, and the golden files under `internal/gen/testdata` are the unit-level
equivalent of `cppgo check`.

**The report.** One line per binding, then the compiler:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

The columns are the C++ declaration, its carrier, the symbol and the reason for
that carrier. When `-library` names a readable file, a direct binding is
cross-checked against the library's dynamic symbol table and its generated
comment ends `, verified against libmathx.so`. The last line names the compiler
that built the shim, which is the earliest warning if it is not the one your
library was built with.

**The evidence line.** Every binding carries one comment naming the C++
declaration, its mangled symbol and the carrier:

```
// C++: mathx::Vector::at(std::size_t) const, _ZNK5mathx6Vector2atEm (thunk cppgo_thunk_79aeb346bd10, not noexcept: an exception would unwind into Go), verified against libmathx.so
```

The mangled symbol is the name the C++ compiler gives the function in the
library's symbol table. C++ has overloading and namespaces, so the compiler
encodes the signature into the name; `_ZN5mathx6Vector5scaleEd` is
`mathx::Vector::scale(double)`.

## Building the shim

The shim is one C++ translation unit compiled by cppgo, not by your build. The
command is assembled from the flags discovered for the header
(`compile_commands.json` or `.clangd`), the compiler named by `-cxx` or found as
`g++`/`clang++`, `-fPIC -shared`, and your library as a link input:

- the library is passed as `-L<dir> -l:<file>`, so the linker records its
  **soname** (or its file name) rather than a build path;
- the shim is linked with `-Wl,-rpath,$ORIGIN`, so the library is found beside
  the shim at run time.

A shim that does not compile is a generation error carrying the compiler's
diagnostic and the binding whose thunk it is in, never a symbol that fails at the
first call.

## Loading: the two objects

The generated package loads **your library first and the shim second**. A shim
symbol is therefore always resolvable; a missing symbol is a load error naming
the shim and the advice to regenerate. A failure to find your library at this
step is tolerated: the shim names it as a dependency, so loading the shim brings
it in anyway.

The shim's `DT_NEEDED` entry names your library by its soname (or, when the
library declares none, by its file name), and cppgo links the shim with
`-Wl,-rpath,$ORIGIN`. `$ORIGIN` in a run path means "the directory this object
was loaded from", so **an object placed beside the shim is found without any
environment variable**. The pairing is recorded in the shim and can be read back
rather than assumed:

```console
$ readelf -d /opt/mytool/libcppgo_mytool.so | grep -E 'NEEDED|RUNPATH'
 0x0000000000000001 (NEEDED)  Shared library: [libmylib.so.1]
 0x000000000000001d (RUNPATH) Library runpath: [$ORIGIN]
```

Your library is looked for in this order, and the first match wins:

1. `CPPGO_LIBRARY_PATH` (the environment);
2. `CppgoLibraryPath` (the package variable);
3. the shim's own directory, when the shim was opened by a path that has one and
   the library sits there;
4. the recorded name, resolved by the dynamic loader's normal search.

Points 1 and 2 are read when the package is opened; a later change is not
observed. `Open("")` uses the name the package was generated with, which the
loader resolves through its own search path — **not** the current directory.

The deployment steps that follow from this are in
[Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).

## Embedded objects

`cppgo.OpenFS` takes a file system of embedded bytes, extracts every `.so` in it
to a private cache, and loads one of them by name. Every `.so` is extracted, not
just the one asked for: the shim's `DT_NEEDED` entry names your library, and the
loader can only find it if both are in one directory. Entries are flattened into
a single directory (base names kept), because that is the only layout in which
`$ORIGIN` sees a sibling. Two entries with the same base name are an error, and
other files are ignored.

```
os.UserCacheDir()/cppgo/<sha256 of the whole set>/<file name>
```

The digest covers every object of the set — name, size and SHA-256 of the
contents — in file-name order. Two programs embedding the same bytes share one
entry; a change to any byte of any object lands in a new directory.

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

The shim must be linked with `-Wl,-rpath,$ORIGIN` for the sibling to be found.
cppgo passes that flag when it builds the shim; a user library that has its own
siblings has to pass it too.

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

The task, the override and what embedding does not buy are in
[Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Callbacks

A Go function handed to C++ is a **slot in a table of five-byte `CALL`
instructions**. The instruction a C caller lands on names the slot; the
trampoline saves the fourteen argument registers and calls
`runtime.cgocallback` through a copy of the runtime's host-ABI entry. The
runtime then runs the Go function on the goroutine that made the call, locked to
its M, with the C frames left where they are.

Three consequences are structural:

- the call that may invoke the callback must be made through the **reentrant**
  carrier; on the direct trampoline the same callback kills the process;
- a callback carries at most six INTEGER and eight SSE arguments and one
  register result, because the trampoline reads the register file only;
- a panic inside the callback is captured, the result is zeroed, and the binding
  re-panics in Go when the foreign call returns. It never unwinds into C++.

## Where the code lives

| Path | Contents |
|---|---|
| [`cppgo.go`](https://github.com/Manu343726/cppgo/blob/main/cppgo.go), [`loadfs.go`](https://github.com/Manu343726/cppgo/blob/main/loadfs.go), [`object.go`](https://github.com/Manu343726/cppgo/blob/main/object.go), [`reentrant.go`](https://github.com/Manu343726/cppgo/blob/main/reentrant.go) | the runtime package the generated code imports: `Open`, `OpenFS`, foreign storage, the reentrant carrier |
| [`internal/asm`](https://github.com/Manu343726/cppgo/blob/main/internal/asm) | the NOSPLIT call trampolines and the stack probe |
| [`internal/callback`](https://github.com/Manu343726/cppgo/blob/main/internal/callback) | C++→Go callbacks through `runtime.cgocallback` |
| [`internal/cxx`](https://github.com/Manu343726/cppgo/blob/main/internal/cxx) | libclang, reached through `purego` |
| [`internal/gen`](https://github.com/Manu343726/cppgo/blob/main/internal/gen) | selection, carrier routing, the Go/asm/shim emitters, the shim compile |
| [`internal/errormap`](https://github.com/Manu343726/cppgo/blob/main/internal/errormap) | the error projection and its configuration (not consumed by the generator yet) |
| [`internal/shim`](https://github.com/Manu343726/cppgo/blob/main/internal/shim) | the construct matrix, measured against a real compiler |
| [`cmd/cppgo`](https://github.com/Manu343726/cppgo/blob/main/cmd/cppgo) | the command line |
| [`examples/`](https://github.com/Manu343726/cppgo/blob/main/examples) | `mathx` and `stats`, each with its own build and tests |
| [`site/`](https://github.com/Manu343726/cppgo/blob/main/site) | the Hugo sources of the documentation site |
| [`docs/adr/`](https://github.com/Manu343726/cppgo/blob/main/docs/adr) | the decision records |

## Related

- Why the split exists: [Why cppgo exists]({{< ref "explanation/why.md" >}}).
- Every carrier, construct and cost: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- What the mechanisms stop at: [Limits]({{< ref "explanation/limits.md" >}}).
- The decisions and their alternatives: [Design decisions]({{< ref "explanation/decisions.md" >}}).
