---
title: "Architecture"
description: "How cppgo works: the artefacts it produces, and one call from Go to C++."
weight: 20
---

**Before this page, read
[The Go runtime and a native call]({{< ref "explanation/go-runtime.md" >}}).**

The design below rests on four mechanisms, and each one is named here before it
is used, because the rest of the page reuses the names.

- A **trampoline** is a handful of instructions of Go assembly that take the
  address of a compiled function and jump to it. It exists because hand-written
  assembly can do one thing ordinary Go code cannot: move the stack pointer, so
  that the callee finds the 16-byte-aligned stack the x86-64 calling convention
  requires.
- **`NOSPLIT`** is the assembler attribute that forbids the Go runtime from
  growing the goroutine's stack at that point of the program. It is what makes
  moving the stack pointer in the trampoline legal: a stack that cannot move
  cannot invalidate the pointer arithmetic three instructions later.
- A **shim** is one C++ file that cppgo generates and compiles beside your
  library. It holds small C++ functions that call the declarations a raw
  register call cannot express, and hand the result back in a form Go can hold.
- A **carrier** is which of the three routes across the boundary one declaration
  takes: the trampoline, a function in the shim, or a third route that lets C++
  call back into Go.

A *foreign call* — a call from Go into machine code that the Go toolchain never
compiled — is what all four serve. The design is what it is because the Go
runtime cannot see one: it cannot move the goroutine's stack in the middle of
it, it cannot hand that goroutine's processor to another goroutine while the call
runs, and the only way back into Go is a callback. Without that model the choices
on this page read as arbitrary.

This is the how-it-works page: it explains the mechanism behind the API. What
you need in order to *use* cppgo is on the other pages — start with
[Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}}), use the
[how-to guides]({{< ref "how-to/_index.md" >}}) for a task, and
[Reference]({{< ref "reference/_index.md" >}}) to look something up.

cppgo is two parts that ship together: a **runtime** that Go programs link, and
a **generator** that writes and builds the C++ side. Everything else in this page
follows from that split and from the three artefacts one generation produces.

## Two parts

The **runtime** is what a generated package imports: dynamic loading, symbol
resolution, the calls into C++, the callback support, the error types. It needs
no C++ compiler and no C++ parser.

The **generator** is the command: it parses the headers, selects the
declarations, decides how each one is called, emits the Go and the C++, and
builds the C++ side. It is the only part that needs libclang — the C library of
the Clang compiler front end, which parses C++ the way the compiler would — and
a C++ compiler.

A program built with a generated package needs neither. It needs the two *shared
objects* at run time — the two `.so` files that the dynamic loader opens — namely
the shim, and the library it links against.

## Three artefacts

A **binding** is one C++ declaration turned into a callable Go function — a
method of a bound class, a package function, a constructor — and it is the unit
this page counts everything in.

| Artefact | Owner | What it is |
|---|---|---|
| the user's library | you | your sources, your compiler flags, your build. cppgo never writes into it |
| the cppgo shim | cppgo | `zz_cppgo_shim.cpp`, generated from the same parse as the Go API and compiled by cppgo into `libcppgo_<package>.so`, linked against your library |
| the generated Go package | cppgo | `zz_cppgo.gen.go` and `zz_cppgo_amd64.s`: the API you call, the assembly trampolines — one per binding, except the [reentrant carrier]({{< ref "explanation/carriers.md" >}}#reentrant), plus the one that frees a copied standard-library result — and the code that loads the shim |

The edge the table draws is the contract: **your build is untouched, and no
generated code is in it.** A header-only library has no shared object at all: the
shim is the whole binary.

## The generated files

```
your header + your compilation configuration
        │
        ▼
   cppgo gen ──► zz_cppgo.gen.go   (the Go API)
        │        zz_cppgo_amd64.s  (one trampoline per binding that does not enter C++ through the runtime, plus the free trampoline)
        │        cppgo_shim/*.cpp  (the thunks)
        │        libcppgo_<pkg>.so (the shim, compiled by cppgo)
        ▼
   your Go program
```

| File | Contents |
|---|---|
| `zz_cppgo.gen.go` | the Go API, a load function, and one declaration per trampoline |
| `zz_cppgo_amd64.s` | one NOSPLIT assembly trampoline per binding, except the bindings on the [reentrant carrier]({{< ref "explanation/carriers.md" >}}#reentrant) — a binding that passes a Go function to C++, and a template binding, enter C++ through `runtime.cgocall` instead — plus `cppgoFree`, the trampoline for the helper that frees a standard-library result the shim copied out. Each trampoline aligns the stack, makes the call, restores it |
| `cppgo_shim/zz_cppgo_shim.h` | the `extern "C"` prototypes of the thunks (`extern "C"` gives a C++ function the C calling convention and an unmangled name, so a plain function pointer can reach it) |
| `cppgo_shim/zz_cppgo_shim.cpp` | the thunks themselves, one per non-direct binding |
| `libcppgo_<package>.so` | the shim, compiled by cppgo and linked against your library |

The first four are sources you commit with your project. The `.so` is a build
output. Nothing in this list is written into your library.

Both trampoline rules come from one place:
[`internal/gen/emit_asm.go`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/emit_asm.go)
walks the bindings, skips the reentrant carrier, and writes `cppgoFree` after
the rest when the shim copies a standard-library result out for Go to free.

A **thunk** is one function in that shim which stands in for one C++ declaration:
the trampoline's register call arrives at it, it calls the real declaration as
C++, and it returns a status instead of letting anything unwind. A binding that
the direct call can express gets no thunk; every other one gets exactly one.

## One call, step by step

Take `mathx::Vector::at`, from
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
double at(std::size_t index) const;
```

It is not `noexcept` — it throws for an index other than 0 or 1 — so the
generator gives it the **thunk** carrier and emits the Go method `At`.

1. **Go calls `At(2)`.** The generated method checks the receiver, calls
   `cppgo.ProbeStack()`
   ([runtime reference]({{< ref "reference/runtime.md" >}}#stack-headroom)), and
   forms a pointer to the local variable that will hold the result.
2. **The trampoline runs.** `cppgoCall_79aeb346bd10` is a NOSPLIT assembly
   function with no Go frame
   ([`zz_cppgo_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo_amd64.s)):

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

   Line by line: `TEXT` declares the function, and `$0-36` says it has no local
   frame and 36 bytes of arguments and result. `MOVQ fn+0(FP), R12` loads the
   address of the C++ function into a register the callee must preserve.
   `this+8(FP)` into `DI` and `a1+16(FP)` into `SI` put the object pointer and the
   index in the first two argument registers of the C convention, and
   `out+24(FP)` into `DX` puts the address of Go storage for the result in the
   third. `MOVQ SP, R13` saves the Go stack pointer and `ANDQ $~15, SP` rounds it
   down to a 16-byte boundary, which is what the System V convention requires at
   the entry of a callee. `CALL R12` makes the call, `MOVQ R13, SP` restores the
   Go stack pointer, and only then does `MOVL AX, ret+32(FP)` store the status the
   callee returned in `AX`. `RET` returns.

   The order is the rule: every argument is read while `SP` still points into the
   Go frame, and the result is stored only after `SP` is back, because `FP` —
   the pseudo-register the arguments are addressed through — is relative to `SP`.
   `ProbeStack` ran first because the probe grows the goroutine stack, and growing
   it after taking a pointer into the caller's frame would move that frame.
   [The Go runtime and a native call]({{< ref "explanation/go-runtime.md" >}})
   derives each of those constraints.
3. **The thunk runs as C++.** The address in `R12` is `cppgo_thunk_79aeb346bd10`
   in the shim
   ([`zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp)):
   it clears the error message this thread recorded — a `thread_local`, the C++
   storage class that gives each thread its own copy — casts `self` back to
   `const mathx::Vector *`, and calls the real method inside `try`/`catch`.
4. **The result comes back through Go storage.** The thunk writes the `double`
   through the out-pointer the caller provided — the address of the Go variable
   step 1 made — and returns `0` or `1`.
5. **The generated method maps the status.** Non-zero becomes a
   `*cppgo.CallError`
   ([runtime reference]({{< ref "reference/runtime.md" >}}#errors)) reading the
   shim's message; zero returns the value.

A **direct** binding is the same path with step 3 replaced by the C++ symbol
itself and no status: `mathx::Vector::scale(double)` is `noexcept` — the C++
keyword by which a function promises not to throw — so the call goes straight to
the function's mangled symbol, `_ZN5mathx6Vector5scaleEd`, and there is nothing
to map back. A *mangled symbol* is the name the C++ compiler encodes into the
library's symbol table, with the namespaces, the class and the parameter types
folded into it; *The evidence line* below reads one apart.

A **reentrant** binding differs at step 2: `cppgo.CallReentrant`
([runtime reference]({{< ref "reference/runtime.md" >}}#calls-and-callbacks))
fills a block of memory with the call's arguments and enters through
`runtime.cgocall` — the Go runtime's own entry point for "this thread is inside
foreign code" — so that a callback can re-enter Go.

In the [`mathx` example](https://github.com/Manu343726/cppgo/blob/main/examples/mathx),
`scale` is the one direct binding: it is `noexcept`, not virtual (`virtual` — the
implementation a call reaches is chosen at run time from the object's type,
instead of being fixed when the call is compiled), defined out of line (in
`src/vector.cpp`, not inside the class body), and the library exports its mangled
name. `x()` is a thunk even though it is a trivial accessor, because its body is
inline and the library exports no symbol for it. The rest can throw or must be
copied, so the shim carries them. The
[carrier table]({{< ref "explanation/carriers.md" >}}) has the full matrix and
the costs.

## Exceptions across the boundary

A declaration that is not `noexcept` is carried by a thunk, and the thunk wraps
the call in `try`/`catch` — the call *and* the conversion of its arguments and
result, because constructing a `std::string` or a `std::vector` there can throw
`std::bad_alloc` too. This is the generated code for `mathx::Vector::norm`
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
`cppgo_last_error`
([the function in the same shim](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp));
the generated binding reads it after a non-zero status and returns it.

**No exception can be supported without a C++ function above the call.** A
thrown exception *unwinds* — it walks the frames above the `throw`, looking for a
`catch` — and it needs unwind information for every frame it crosses. A Go frame
has none, so an exception that reaches one terminates the process. A direct
binding is `noexcept` by construction, so it never has a handler; that is why a
throwing function is never assigned the direct carrier, even when its signature
would fit the registers.

The practical consequence for a user is on
[Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}): an API that
throws and whose headers the shim cannot include cannot be bound safely, and the
fix belongs in the library.

**A status-code convention.** Some libraries report failure as a 32-bit return
code instead of an exception. A declaration named with `-status` is generated as
a call whose status **is** the error channel: the C++ side returns the callee's
own code to Go, and Go treats any non-zero value as the failure. The Go
signature drops the integer and keeps only the error. A `-status` entry that
names no declaration, or whose declaration returns a value rather than an
integer, is a generation error.
[Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}#a-status-code)
has the steps.

## Values, copies, and the layout check

A **record** — C++'s word for a struct or class used as a bundle of fields — is
**mirrorable** when all its fields are public, its layout is the natural Go
layout, and no field is a bitfield (several fields packed into the bits of one
integer), a reference, a pointer, a union member (a *union* lets several members
share the same storage) or a non-mirrorable type. The record in the examples is
`mathx::Point`, in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
struct Point {
    double x;
    double y;
};
```

Its Go value *is* the C layout — the bytes of the Go struct are the bytes C++
sees — and the generated file asserts that at compile time:

```go
// The C++ layout is proven at compile time: a wrong size, alignment or
// field offset does not compile.
var _ [16]byte = [unsafe.Sizeof(Point{})]byte{}
var _ [8]byte = [unsafe.Alignof(Point{})]byte{}
var _ [0]byte = [unsafe.Offsetof(Point{}.X)]byte{}
var _ [8]byte = [unsafe.Offsetof(Point{}.Y)]byte{}
```

Each line is a compile-time assertion: the array length is the measured value and
the fixed number beside it is what C++ layout requires, so a mismatch is an array
whose length is not a constant — a build error, not a run-time bug.

A record that is not mirrorable becomes an opaque value instead: Go storage the
C++ side owns, with `Close`, `Clone` and generated accessors.

By-value records are copied by the shim in both directions. `mathx::midpoint`,
for example, takes a 16-byte **POD** by value and returns one — a POD, for *plain
old data*, being a struct with no constructor, no virtual function and no private
members, whose bytes are its whole representation — so the thunk copies the
arguments into the call and the result back into Go storage.

## The `std::` ABI

The `std::` in that heading is the C++ standard library's namespace, and the
*ABI* — *application binary interface* — is how a compiled call and a compiled
value are laid out: which registers and which memory carry a call's arguments,
and what bytes a value has in memory. The standard library's types never appear
in Go for that second reason: their layout exists only in the shim, which is C++
and can legally construct them:

- a `std::string` parameter is copied into a `std::string` in the shim, and a
  result is copied out into a Go string;
- a `std::vector<T>` is copied element by element in both directions;
- `std::string_view` and `std::span` — read-only *views*, a pointer and a length
  that own nothing — are borrowed as `(pointer, length)` and are valid only for
  the call;
- the shim and your library must use the same standard library and the same
  `_GLIBCXX_USE_CXX11_ABI`, the build macro that selects which of two
  incompatible `std::string` layouts the GNU standard library compiles. cppgo
  compiles the shim with the flags it discovered and reports the compiler it
  used; it does not inspect the library's standard library, so keeping the two in
  step is a build decision.
  Recording the version and `_GLIBCXX_USE_CXX11_ABI` and refusing a mismatch is
  designed ([Limits]({{< ref "explanation/limits.md" >}})).

## Templates: why two phases

A C++ template does not exist until it is instantiated: the compiler only produces
code for a template when some code names the concrete types it is used with.
`std::vector<int>` and `std::vector<double>` are two different types with two
different pieces of machine code, and which ones your program needs is a property
of the *Go* program, not of the header.

That is why templates take two phases. `cppgo gen` writes the generic Go API,
which compiles without any concrete type. `cppgo build -scan <package>` loads the
program with `go/packages` — the Go library that answers "what does this program
import and use" — reads the type arguments at every use of the generic API, and
for each specialisation (one template with one set of concrete types) writes the
explicit instantiation into the shim's source:

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
`.clangd`, the *compilation database* in which a C++ project records the exact
compiler command for each source file. cppgo rewrites a database's relative
include paths against the directory its entry names, because libclang resolves a
relative path against cppgo's own working directory, which is a different
directory: the parse then succeeds while seeing a different program.

**The libclang floor.** The generator needs libclang 20 or newer. It reads the
offset of base-class subobjects through `clang_getOffsetOfBase`, which libclang
19 does not export, so a build against 19 fails at load time with
`undefined symbol: clang_getOffsetOfBase`.

**Selection.** A header is the unit of selection: every public declaration in it,
plus the *type closure* of every bound signature — the types that signature
mentions, and the types those mention, transitively. `-bind` and `-exclude` narrow
the set, and an entry that names nothing is an error. A construct this build does
not support is refused with a message naming it and the fix, which is what
[Limits]({{< ref "explanation/limits.md" >}}) tabulates.

**Determinism.** The same inputs produce byte-identical Go and C++. Bindings are
sorted by qualified name, and a thunk's name is a stable hash of the qualified
signature.

**The report.** One line per binding, then the compiler:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

The columns are the C++ declaration, its carrier, the symbol the call goes to and
the reason for that carrier — `register class` there saying that the values fit
the argument registers, so the call needs no memory on the way in. When
`-library` names a readable file, a direct binding is cross-checked against the
library's dynamic symbol table — the list of symbols a shared object publishes to
the loader — and its generated comment ends `, verified against libmathx.so`. The
last line names the compiler that built the shim, which is the earliest warning
if it is not the one your library was built with.

**The evidence line.** Every binding carries one comment naming the C++
declaration, its mangled symbol and the carrier:

```
// C++: mathx::Vector::at(std::size_t) const, _ZNK5mathx6Vector2atEm (thunk cppgo_thunk_79aeb346bd10, not noexcept: an exception would unwind into Go), verified against libmathx.so
```

Read the mangled symbol as the signature: C++ has overloading and namespaces, so
the compiler encodes the parameter types and the qualifying names into the
symbol it emits. `_ZN5mathx6Vector5scaleEd` is `mathx::Vector::scale(double)`.

## Building the shim

The shim is one C++ translation unit — one `.cpp` file, compiled as a unit — built
by cppgo, not by your build. The command is assembled from the flags discovered
for the header (`compile_commands.json` or `.clangd`), the compiler named by
`-cxx` or found as `g++`/`clang++`, `-fPIC -shared` (`-fPIC` makes the code
usable from whatever address it is loaded at, `-shared` asks the linker for a
shared object), and your library as a link input:

- the library is passed as `-L<dir> -l:<file>`, so the linker records its
  **soname** — the name written inside a shared object that the loader resolves
  at run time, normally a versioned file name like `libfoo.so.1` — or its file
  name when it declares no soname, rather than a build path;
- the shim is linked with `-Wl,-rpath,$ORIGIN`, so the library is found beside
  the shim at run time.

A shim that does not compile is a generation error carrying the compiler's
diagnostic and the binding whose thunk it is in, never a symbol that fails at the
first call.

## Loading: the two objects

Loading happens through the dynamic loader — the system component that opens a
shared object at run time and resolves the names in it — and the generated
package loads **your library first and the shim second**. A shim symbol is
therefore always resolvable; a missing symbol is a load error naming the shim
and the advice to regenerate. A failure to find your library at this step is
tolerated: the shim names it as a dependency, so loading the shim brings it in
anyway.

The shim's `DT_NEEDED` entry — the list of other shared objects a shared object
declares it needs, which the loader opens with it — names your library by its
soname (or, when the library declares none, by its file name), and cppgo links
the shim with `-Wl,-rpath,$ORIGIN`. `$ORIGIN` in a run path means "the directory
this object was loaded from", so **an object placed beside the shim is found
without any environment variable**. The pairing is recorded in the shim and can
be read back rather than assumed:

```console
$ readelf -d /opt/mytool/libcppgo_mytool.so | grep -E 'NEEDED|RUNPATH'
  0x0000000000000001 (NEEDED)  Shared library: [libmylib.so.1]
  0x000000000000001d (RUNPATH) Library runpath: [$ORIGIN]
```

(`readelf -d` prints a shared object's dynamic section, the table the loader
reads; the two `grep` patterns select the dependency list and the run path.)

Your library is looked for in this order, and the first match wins:

1. `CPPGO_LIBRARY_PATH` (the environment);
2. `CppgoLibraryPath` (the package variable);
3. the shim's own directory, when the shim was opened by a path that has one and
   the library sits there;
4. the recorded name, resolved by the dynamic loader's normal search.

Points 1 and 2 are read when the package is opened; a later change is not
observed. `Open("")` — the load function the generated package exports — uses the
name the package was generated with, which the loader resolves through its own
search path — **not** the current directory.

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
  object. `O_EXCL` means "create the file, or fail if it already exists", which
  is what keeps two processes from writing the same name;
- an existing file is reused only when its size and SHA-256 both match. A
  truncated or corrupted entry is replaced;
- a path that is not a private directory, or a symbolic link where an object
  should be, is refused with `cppgo.ErrInsecureCache`
  ([runtime reference]({{< ref "reference/runtime.md" >}}#embedded-objects))
  instead of being used. Those are shapes another local user could have prepared
  in advance.

The shim must be linked with `-Wl,-rpath,$ORIGIN` for the sibling to be found.
cppgo passes that flag when it builds the shim; a user library that has its own
siblings has to pass it too.

The task, the override and what embedding does not buy are in
[Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Callbacks

A Go function handed to C++ becomes a **callback slot**: one entry in a
process-wide table of identical five-byte `CALL` instructions. C++ receives that
entry's address and calls it like any C function pointer, and the `CALL` lands in
one shared assembly routine. The routine works out which slot it was entered
through — the slots sit a fixed distance apart, so the return address identifies
one — saves the six integer and eight floating-point argument registers, and
calls `runtime.cgocallback`, a copy of the Go runtime's own entry for a callback
arriving from C. The runtime then runs the Go function on the goroutine that made
the original call, on the same operating-system thread (an **M**, in the
runtime's vocabulary), with the C frames left where they are.

Three consequences are structural:

- the call that may invoke the callback must be made through the **reentrant**
  carrier, because the runtime accepts a callback only on a thread that is
  already inside a foreign call; on the direct trampoline the same callback kills
  the process;
- a callback carries at most six INTEGER and eight SSE arguments and one
  register result, because the routine reads the *register file* only — the fixed
  set of CPU registers the calling convention passes arguments in, and nothing on
  the stack;
- a panic inside the callback is captured, the result is zeroed, and the binding
  re-panics in Go when the foreign call returns. It never unwinds into C++.

## Related

- Why the split exists: [Why cppgo exists]({{< ref "explanation/why.md" >}}).
- Every carrier, construct and cost: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- What a call costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
- What the mechanisms stop at: [Limits]({{< ref "explanation/limits.md" >}}).
