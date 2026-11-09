# cppgo, design contract (v4)

**Who this is for.** A contributor who writes Go and C++ and has not worked on cppgo before. It
answers one question: **what is v4, what must it support, and what does it refuse?** Every mechanism
stated here is either named with the test in this repository that measures it, or marked
**designed**, which means the row states the next workstream's specification rather than a
measurement. A mechanism this file does not state is not part of the design, and is not to be
invented: raise it with the orchestrator instead.

The choices behind the contract are argued in their own records, and this page does not re-argue
them: [ADR-0001](docs/adr/0001-header-first-bindings.md) binds C++ headers rather than Go
declarations, [ADR-0002](docs/adr/0002-cppgo-built-shim.md) gives the shim to cppgo's own build and
supersedes ADR-0001, and [ADR-0003](docs/adr/0003-embedded-libraries.md) embeds the C++ side in the
Go binary.

## The words this document uses

Each of these names a mechanism, and each is defined here once so that every section below can use it
without re-explaining it.

- **FFI** — a *foreign function interface*: a call from one language into code that a different
  compiler produced, made through the platform's C calling convention.
- **binding** — one generated Go function or method together with the C++ declaration it calls. It is
  the unit the generator selects, assigns a carrier to, emits and reports.
- **ABI** — the *application binary interface*: the layout of one compiled call, that is, which
  registers and which memory carry the arguments and the result. Unless a section says otherwise,
  this document means the Itanium C++ ABI as implemented on x86-64 Linux, stated in full in § *The
  ABI the direct path calls*.
- **translation unit** — one C++ source file together with everything it includes, which is the unit
  a C++ compiler compiles at a time.
- **trampoline** — the Go assembly function `cppgo gen` emits for one binding. It loads the argument
  registers, calls a single foreign address, stores the register result and returns: it is the
  function that actually crosses the boundary. § *The trampoline* shows one and names every
  instruction in it.
- **shim**, **thunk** — the *shim* is the C++ translation unit that `cppgo gen` writes and cppgo
  compiles into a shared object of its own. A *thunk* is one function inside it: an `extern "C"`
  stub that calls one real C++ declaration the way C++ itself would, for the declarations a
  trampoline cannot express. § *Carriers* says which ones those are.
- **carrier** — which mechanism performs a given call: a direct trampoline, a thunk in the shim, or
  the *reentrant* call, the only one that can host a Go callback. Every binding gets exactly one
  carrier, assigned before anything is emitted (§ *Carriers*).
- **POD** — *plain old data*: a struct with no constructors, no virtual functions and a standard
  layout, whose bytes may be copied safely. `metrics::Point` in
  [`examples/full/include/metrics/metrics.hpp`](examples/full/include/metrics/metrics.hpp) is one.

## What v4 changes about v3

One thing: **who builds the shim**. v3 wrote `zz_cppgo_shim.cpp` for the *user's* build to compile
into their library, which made a cppgo-generated file a dependency of a build outside cppgo's
ownership. v4 has **cppgo generate and compile the shim itself**, into an object of its own that
links the user's library. The user's sources and build are never modified and never include
generated code. [ADR-0002](docs/adr/0002-cppgo-built-shim.md) records why the change was made and
what it costs.

## What this is

`cppgo` calls C++ from Go with **no cgo at build time** and **no `import "C"`**. *cgo* is Go's own
way of calling C: it needs a C toolchain, `CGO_ENABLED=1` and a Go file that imports `"C"`, and
avoiding it is what cppgo exists for. The primary path points cppgo at the headers and the
compilation configuration, and it generates the equivalent Go API; the reader writes no binding
declarations. The headers are read through *libclang*, the C library of the Clang compiler front
end, which answers questions about a C++ declaration — its type, its size, whether it is virtual —
without compiling it. A hand-written declaration file remains as a fallback for a project that
keeps libclang out of its generation step. It is a separate, documented mode outside the product.

Three claims, each measurable in this repository:

- **complete**: every construct in the bound headers is reachable, including the ones the System V
  AMD64 calling convention cannot express — the x86-64 rule that passes the first arguments in
  registers (`RDI, RSI, …`) and the rest on the stack.
- **Go-side generated**: the artefacts cppgo emits are Go, Go assembly, and one C++ translation unit
  that cppgo compiles itself. The user's build sees none of them.
- **measured where it claims**: every row of the mechanism table below either names the test in
  [`internal/callback`](internal/callback) or [`internal/shim`](internal/shim) that measures it, or
  says **designed** and names the v3 behaviour it is derived from. A designed row states the next
  workstream's specification, and every hard limit is stated with the measurement that found it.

## The three artefacts

*Artefact* is this document's word for a build output with an owner. Three of them make up a bound
package, and the edge between the first and the other two is the contract.

| artefact | who owns it | what it is |
|---|---|---|
| **the user's library** | the user | their sources, their compiler flags, their build. `libuser.so` in the tests. cppgo never writes into it and never asks it to include anything |
| **the cppgo shim** | cppgo | `cppgo_shim/zz_cppgo_shim.cpp` and its header, generated from the same header parse as the Go API, **compiled by cppgo** into `libcppgo_<package>.so` — `libcppgo_user.so` for `-package user` — and linked against the user's library. One `extern "C"` thunk per binding the direct ABI cannot express. `extern "C"` is the C++ linkage specification that switches off C++ name mangling — the compiler's encoding of namespaces, classes and overloads into one symbol name — so the thunk's symbol is the plain C name a `dlsym` lookup can find |
| **the generated Go package** | cppgo | the API the caller sees, plus one `NOSPLIT` trampoline per thunk and per direct symbol. A `NOSPLIT` function is one the Go assembler marks as having no stack-growth preamble: it may not grow the stack, and the runtime never preempts it asynchronously. The package loads the shim, which pulls the user's library in through its `DT_NEEDED` entries — the list, recorded in a shared object, of the objects the dynamic loader must map alongside it |

The dotted line is the contract's edge: **the user's build is untouched, and generated code is not in
it.** Everything else follows from that line.

**Status of the tree.** This contract states the target. The mechanisms it relies on are measured and
have tests in this repository ([`internal/callback`](internal/callback) for the reverse direction,
[`internal/shim`](internal/shim) for the construct matrix, [`internal/errormap`](internal/errormap)
for the error projection and its configuration, and the emitters' existing direct-carrier selection
with its own tests). The shim rewrite has landed: `cppgo gen` renders `zz_cppgo_shim.cpp`, compiles
it (`buildShim` in [`internal/gen/build.go`](internal/gen/build.go)), and writes the object beside
the generated package. What [`internal/gen`](internal/gen) and [`cmd/cppgo`](cmd/cppgo) do **not** do
yet, and therefore what the next workstreams owe:

1. **the error pass**: call `Apply` on the package's `*errormap.Config` over the parsed signatures
   (with the shape inference fed from libclang's members), and refuse to emit when a declaration can
   fail and its convention is neither inferred nor configured.
2. **the thunk error-block templates**: the `try`/`catch`, the status-and-block shape, the *pinned*
   block the thunk writes into — pinned meaning the collector has been told the storage may not
   move, because C++ holds its address — and the cloned message, as `internal/errormap`'s fixtures
   write them by hand.
3. **the Go side of the mapping**: `(T, error)` signatures from the rules, and the `ErrorCode` /
   `Message` constructors.
4. **`cppgo.yaml` as a generation input**, so `cppgo check` covers it.

Until then the error bindings in `internal/errormap` are written by hand, and every statement in
§ *Errors from C++ to Go* is the emitter's contract rather than a description of `internal/gen`.

Consequences worth stating:

- the user's `CMakeLists.txt`/`Makefile` gains no step. A project that only wants to *consume* a
  cppgo-generated package installs two shared objects instead of one.
- the shim and the user's library must agree on the C++ ABI and the standard library, because the shim
  calls into the library across that boundary. Two C++ standard libraries are in common use, GCC's
  libstdc++ and LLVM's libc++, and they are not interchangeable. The generator compiles the shim
  with the flags it discovered for the header — from `compile_commands.json`, the *compilation
  database* a C++ build can be asked to emit, which records the compiler and the exact flag list of
  every translation unit, and from `.clangd` — so a `-D_GLIBCXX_USE_CXX11_ABI=…` the library was
  built with reaches the shim as well. What the generator does **not** do yet is read the library's
  standard library back out and refuse a mismatch (see § *The `std::` ABI*).
- a header-only library with no `.so` at all is a supported case. The shim is all there is, and the
  "library" the generated package loads is the shim.

## The two layers

The runtime and the generator stay separate products with separate dependencies, and the line between
them is part of this contract.

| layer | package | owns | may depend on |
|---|---|---|---|
| **runtime** | `github.com/Manu343726/cppgo` (repository root) | dynamic loading, symbol resolution, the C ABI and its trampolines, the reentrant carrier, stack probing, foreign-object storage, wire helpers, error types, the callback table, and the dependency walk that preloads an embedded set | the standard library, [`internal/asm`](internal/asm), [`internal/rtl`](internal/rtl), [`internal/callback`](internal/callback), [`internal/elfdeps`](internal/elfdeps), `purego` |
| **generator** | [`internal/gen`](internal/gen), [`internal/cxx`](internal/cxx), [`cmd/cppgo`](cmd/cppgo) | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim, **and the shim's own C++ compile** | anything, including libclang through `purego` and the C++ compiler |

*purego* is the third-party Go library that calls `dlopen` and `dlsym` without cgo: `dlopen` is the C
library call that maps a shared object into the running process, and `dlsym` is the one that looks a
symbol up inside it. The runtime uses purego's loader and never its call path.

The runtime **never imports `internal/cxx`, directly or transitively, and has no libclang
dependency**. libclang is generation-time only. The runtime builds and its tests run with
`CGO_ENABLED=0` on a machine with no libclang installed. The generator needs libclang and says so with
`ErrLibclangNotFound` when it is absent, and needs a C++ compiler to build the shim and says so with a
message naming the compiler when it cannot find one.

What generated code uses from the runtime, with no other dependency:

```go
func Open(path string) (*Library, error)     // purego dlopen/dlsym underneath
func (l *Library) Symbol(name string) (uintptr, error)
func (l *Library) Close() error
func (l *Library) Path() string

func ProbeStack()                            // StackReserve bytes of goroutine stack headroom
const StackReserve = 32 << 10

func NewObject(size, align uintptr, release func()) *Object
func (o *Object) Addr() uintptr
func (o *Object) Retain()
func (o *Object) Release()

func Ptr[T any](s []T) unsafe.Pointer       // wire helpers
func StrPtr(s string) unsafe.Pointer
func KeepAlive(...any)
func LastError(lib *Library) error

type Handle uintptr                          // low-level only; never in a generated signature
```

The signatures above are the whole surface; the comments name the mechanism behind each. *Wire
helpers* are the three small functions — `Ptr`, `StrPtr`, `KeepAlive` — that hand a Go slice or string
to C++ as a bare pointer and keep the Go value alive until the call returns.

The generated assembly, and nothing else, also uses `asm.Call` (the direct carrier),
`asm.CallReentrant` (the callback-capable carrier), `asm.Probe`, and `callback.Register` /
`callback.Release` / `callback.TakePanic`.

`Object` pins its storage with `runtime.Pinner`. Pinning is telling the garbage collector that an
address must keep pointing at the same bytes: the collector may otherwise move an object, and it
cannot see a pointer that C++ holds. There is no package-level object registry. The one
piece of process-global state in the runtime is the callback table, and § *Callbacks* states why it
cannot be anything else.

## Carriers

Every binding is assigned exactly one **carrier** before anything is emitted. The generator never
leaves a declaration unbound and never emits a call it has not established a carrier for.

### Direct

The generated trampoline calls the *mangled* C++ symbol in the user's library. The mangled name is
the compiler's encoding of a C++ name into one symbol, which is how namespaces, classes and overloads
survive in the object file: `user::checked_div(int,int)` becomes `_ZN4user11checked_divEii`. Measured
at 15.9 ns/op against 151 ns/op for the reentrant carrier on the same trivial callee (the benchmarks
in [`internal/callback`](internal/callback)). All of these must hold:

- the declaration has an out-of-line body and its mangled name is in the user's `.dynsym`, the
  dynamic symbol table the loader resolves against; the generator reads it with
  `cxx.DefinedDynamicSymbols` in [`internal/cxx/elf.go`](internal/cxx/elf.go). As in v3, a
  header-only declaration whose mangled name happens to be exported is direct too, and that rule is
  kept, with its tests.
- it is **not virtual** (the call goes to a fixed address, so dispatch is a shim concern).
- it is **`noexcept`** (the direct path has no handler, and an escaping exception terminates the
  process, measured).
- the signature is register-representable: scalars, enums, pointers and references, and a POD of at
  most 16 bytes with no MEMORY-class part. A *MEMORY-class* value is one the ABI passes in memory
  instead of registers, because it is too large or not trivially copyable. No `std::` type.

### Shim

Everything else is carried by a thunk in `zz_cppgo_shim.cpp`. The thunk is C++ compiled by cppgo against the same headers and the same flags, and linked against the user's library:

| what the declaration is | what the thunk does | measured by |
|---|---|---|
| inline / header-only body | includes the header and calls it: the body is compiled into the shim | `TestInlineHeaderOnlyIsReachableOnlyThroughTheShim` |
| virtual method, virtual destructor | calls it as C++ does — real dispatch through the vtable, the per-class array of function pointers that every polymorphic object points at | `TestVirtualDispatchAndVirtualDestructor` |
| template | instantiates the concrete *specialisation* — the version of the template with its parameters filled in — and calls it | `TestTemplateInstantiation` |
| `std::string` / `std::vector` | constructs the C++ value, converts, copies in or out | `TestStdStringAndVector` |
| `string_view` / `span` / `array` / `optional` / `unique_ptr` / `shared_ptr` | constructs the C++ value, converts, copies in or out | designed, not measured (see § *The per-construct mechanism table*) |
| POD above 16 bytes / MEMORY class by value | copies it into the call; a result is written through the out-pointer the Go side supplies | `TestByValueRecordShape` (a 24-byte result), `TestByValueRecordShimCompiles` |
| non-POD by value | the thunk passes the object Go holds, so C++ copies it into the call; a result is constructed into Go's pinned storage with placement new (the C++ form that constructs an object inside storage the caller already allocated, rather than allocating again), and `Close` runs the declared destructor once | `TestNonPODByValueRecordShape`, `TestNonPODByValueRoundTrip` |
| exception-throwing call | wraps the call in `try`/`catch`, converts `std::exception` and `...` into a message held in thread-local storage, one instance per operating-system thread | `TestExceptionsBecomeErrors` |
| multiple inheritance / virtual base | performs the `static_cast`/`dynamic_cast` that adjusts `this` | `TestMultipleInheritanceAdjustment` |
| more than 6 INTEGER or 8 SSE arguments | takes one pointer to an argument block and unpacks it; Go and C++ both assert the block's size and every offset | `TestArgumentBlockShape`, `TestArgumentBlockRoundTrip` |
| anything with an out parameter | writes through the pointer the Go side provides | every row above |
| a callback parameter | constructs the `std::function` from the Go function pointer | `TestCallbackThroughTheShim` |

The direct carrier and a shim thunk are both ordinary C ABI functions from Go's point of view: `Open`
resolves a `uintptr`, `ProbeStack` runs, and one NOSPLIT trampoline calls it. The carrier is invisible
in the API and visible in the report, one line per binding: the C++ signature, the carrier, the symbol
Go calls, and the reason the carrier was chosen. The carrier column carries `direct` or `thunk`, and
the symbol column the thunk's name (`cppgo_thunk_<hash>`) or the mangled symbol. A run over
[`internal/gen/testdata/mathx/vector.hpp`](internal/gen/testdata/mathx/vector.hpp), excerpted from
the nine bindings it reports, with the scratch paths elided as `…`:

```
$ cppgo gen -header internal/gen/testdata/mathx/vector.hpp -package mathx -out /tmp/…/mathx -library /tmp/…/libmathx.so
mathx::Vector::Vector(double, double)        thunk  cppgo_thunk_7d8c482bce3e     not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, out of line, not virtual, register class
mathx::Vector::x() const                     thunk  cppgo_thunk_00064a4bc112     not noexcept: an exception would unwind into Go
…
cppgo: built /tmp/…/mathx/libcppgo_mathx.so with /usr/bin/g++
```

### The callback-capable call

A binding whose signature takes a function pointer or a `std::function` (the standard library's
type-erased callable wrapper) is called through `asm.CallReentrant` instead of `asm.Call`. The
carrier is *reentrant* because C++ may call back into Go during the call, and Go code reached that
way may make another foreign call in turn. There are two runtime entries:

- `asm.Call` calls the C function from a NOSPLIT trampoline on the goroutine's stack. Fast, and the
  only carrier a plain function call needs.
- `asm.CallReentrant` calls `runtime.cgocall` with a generated assembly wrapper that loads the six
  INTEGER and eight SSE registers. `runtime.cgocall` is the Go runtime's own entry point for entering
  foreign code — what cgo's generated code calls — and cppgo reaches it by `//go:linkname`, the
  compiler directive that names an unexported symbol directly, so the build still has no cgo. The
  call runs on an `M`, the runtime's name for one operating-system thread, and leaves that thread
  marked as being inside a foreign call. That is exactly the state `runtime.cgocallback` (the
  runtime's entry point for C code calling back into Go) requires.

A callback invoked through the direct carrier kills the process. The runtime's callback entry then
returns to Go on an `M` that never entered a foreign call, and it dies in `exitsyscall` — the runtime
function that hands a thread back from a system call to running Go code — with
`fatal error: morestack on g0`, `g0` being the stack the runtime uses when it is not running
goroutine code. That is measured in a child process by `TestCallbackOnTheDirectTrampolineIsFatal`. It
is why the carrier choice is part of the contract and not an optimisation.

The reentrant carrier runs the C frames on the M's system stack. `ProbeStack`'s 32 KiB reserve
protects the goroutine stack instead. `TestReentrantCarrierStackBudget` puts the same 28 KiB frame
through both carriers, and both reach the fixture's result. The budget for the reentrant carrier is
the thread stack, while the direct carrier relies on the 32 KiB the probe guarantees. It costs one
16-byte allocation per call, because the argument block that carries the target cannot live on the
goroutine's stack: a callback may grow that stack, and the C side holds the block's address.

## What the user writes

**Primary path with no declarations.** The input is the headers plus the compilation configuration:

```
cppgo gen -header include/user.hpp -package user -out ./user
          [-library libuser.so] [-compile-commands build/compile_commands.json]
          [-instantiate '<template specialisation>']... [-rename '<signature>=<GoName>']...
```

Each argument names one input, and a bracket means the argument is optional:

- `-header` is a C++ header to bind, repeatable. The header is the unit of selection: everything
  public in it is bound.
- `-package` is the name of the Go package to generate, and `-out` the directory it is written to.
- `-library` is the user's shared object. The shim is linked against it and the direct carrier is
  looked for in it. Omit it for a header-only library, where the shim is the whole binary.
- `-compile-commands` is the compilation database the header's flags are read from.
- `-instantiate` names a template specialisation to emit, and `-rename` resolves a Go name collision.
  Both are input only where the C++ leaves the answer unstated.

`cppgo gen` parses the headers through libclang, selects every public declaration, emits the Go
package and the shim, compiles the shim, and writes the invocation into the generated file so the
regeneration command lives in the repository. `cppgo check` runs the same work, writes no output, and
exits non-zero when a file is stale. `cppgo build` is phase two of § *Templates and the two-phase
generics flow*: it reads the Go packages named by `-scan`, emits the shim thunks for the template
instantiations they use, and compiles the shim again. Templates have a section of their own because
they are the only construct that needs this second command.

**Fallback, a declaration file.** For a project that avoids libclang, the same emitters take a
hand-written description of the symbols and signatures to bind. It produces the same artefacts and is
documented separately. It is never the recommended path, and the header is still what the shim
includes.

## The generated API

The input is C++. The declarations below condense the fixture cppgo's own shim tests bind:
[`internal/shim/testdata/user.hpp`](internal/shim/testdata/user.hpp) is the real header, and it also
carries the second base class, the non-POD value and the `std::function` parameter that § *The
per-construct mechanism table* and § *Callbacks* measure. Condensed to the shapes this section is
about, it is:

```cpp
#include <cstdint>
#include <string>
#include <vector>

namespace user {

struct Point { double x, y; };

class Shape {
public:
    virtual ~Shape();
    virtual double area() const;
};

class Circle : public Shape {
public:
    Circle(double r);
    ~Circle() override;
    double area() const override;
    double radius() const;
};

inline double twice(double v) { return 2 * v; }      // header-only

std::string greet(const std::string &prefix, const std::string &name);
std::vector<double> doubled(const std::vector<double> &values);
std::int32_t checked_div(std::int32_t a, std::int32_t b);

} // namespace user
```

`cppgo gen` writes this package, and this is the API. The bodies are elided, every function is
generated in full, and the `// C++:` evidence comments are abbreviated: the real line also carries
the carrier and the reason the generator chose it (§ *Generation. Input, selection, evidence,
output*).

```go
// Code generated by cppgo gen. DO NOT EDIT.
// cppgo gen -header include/user.hpp -package user -out ./user

package user

import "github.com/Manu343726/cppgo"

// Point is user::Point: a POD, 16 bytes, fields at offsets 0 and 8.
type Point struct {
	X float64
	Y float64
}

// Circle is user::Circle. The object belongs to C++; obj is where it lives.
type Circle struct{ obj *cppgo.Object }

// NewCircle constructs a user::Circle.
//
// C++: user::Circle::Circle(double), _ZN4user6CircleC1Ed
func NewCircle(r float64) (*Circle, error)

// Close destroys the object. It returns cppgo.ErrClosed when the receiver has
// no live object: a nil pointer, the zero value, or a second Close.
func (c *Circle) Close() error

// Area calls the virtual method: the shim dispatches through the vtable, so a
// Circle reached as a *Shape runs Circle's override.
//
// C++: user::Circle::area() const, _ZNK4user6Circle4areaEv
func (c *Circle) Area() (float64, error)

// Radius is the non-virtual member.
func (c *Circle) Radius() (float64, error)

// Twice is the header-only inline function. It goes through the shim.
func Twice(v float64) (float64, error)

// Greet copies the Go strings into std::strings and the result back out.
func Greet(prefix, name string) (string, error)

// Doubled copies the slice into a std::vector and the result into a fresh slice.
func Doubled(values []float64) ([]float64, error)

// CheckedDiv returns an error when the C++ function throws.
func CheckedDiv(a, b int32) (int32, error)

// AsShape returns the same object seen as its base. The shim performs the cast,
// so a non-zero base offset is never computed in Go.
func (c *Circle) AsShape() *Shape
```

`*cppgo.Object` is the runtime's pinned storage for a value C++ owns (§ *The two layers*). The
listing omits the `Shape` type, which has the same shape as `Circle`.

Rules that the API shape follows:

- **Mirrorable record → Go struct with matching exported fields.** A *record* is a C++ struct or
  class whose data members are what it is. It is *mirrorable* when its fields are all public, its
  layout is the natural Go layout, and no field is a bitfield (a member packed into a few bits of a
  word), a reference, a pointer, a union member, or a non-mirrorable type. The generator reads
  `sizeof`, `alignof` and every offset from libclang and asserts them at compile time in the generated
  file.
- **Every other record → opaque value type**: a Go struct with unexported storage, `Close()` from the
  destructor, `Clone()` from the copy constructor (when one exists), and generated accessors for every
  public field (`Field()` / `SetField(v)`). Bitfields, packed layouts, private members, non-POD
  members, and a layout libclang could not complete all take this path. Every construct stays reachable.
- **Class → Go struct with methods**, `New<Type>(...) (*Type, error)` for a constructor, `Close() error`
  for the destructor, package functions for free and static functions, named Go integer types for
  enums.
- **`cppgo.Handle` appears in no exported signature.**
- **Ownership and views.** An owning value carries the destructor of the type it was constructed as and
  a Go-side reference count. A *view* is a Go value that names the same C++ object without owning it:
  `As<Base>()` returns one and adds a reference. `Close` drops one reference, and the destructor runs
  at the last one.
- **Zero values are invalid**: a method on a nil pointer or a closed value returns `cppgo.ErrClosed`.
  No finalizer is generated.

## The per-construct mechanism table

Every row names the mechanism, the evidence, and the limit when there is one. *Direct* means the
generated trampoline calls the user's symbol; *shim* means the cppgo-built thunk calls it as C++.

| # | construct | mechanism | evidence | limit, if any |
|---|---|---|---|---|
| 1 | virtual dispatch | virtual calls go to the shim (`object->method()`), where the vtable is. Direct vtable dispatch from Go also works: the *vptr* — the hidden first word of a polymorphic object, a pointer to its class's vtable — points at the vtable's *address point*, where the virtual destructor's two words (complete and deleting) come first, so a virtual method's address is the next word, at `vptr + 8k` for `k ≥ 2` | `TestVirtualDispatchAndVirtualDestructor`, `TestVirtualDispatchFromTheVTable`, plus a C++ dump of the same vtable | the *slot index* is generation-time knowledge (declaration order); a slot call from Go is never needed when the shim exists, and is kept as the direct path for an exported non-virtual-looking wrapper |
| 1 | virtual destructor | `delete` through the base pointer in the shim; the derived destructor ran (counter read back) | `TestVirtualDispatchAndVirtualDestructor/virtual_destructor_reaches_the_derived_one` | — |
| 1 | `this` adjustment, multiple inheritance | the shim's `static_cast`; `Disk`'s `Circle` subobject is at a non-zero offset and both addresses are returned to Go | `TestMultipleInheritanceAdjustment` | virtual bases: the offset is a vtable lookup, done in C++ by the same cast; it is not computed in Go |
| 2 | inline / header-only | shim includes the header; the body is compiled into the shim | `TestInlineHeaderOnlyIsReachableOnlyThroughTheShim` (the user's library exports no symbol, the shim does) | a declaration whose definition is not visible to the shim cannot be bound: the user must include the header that defines it, or provide an out-of-line definition |
| 3 | `std::string` | copied in and out through a buffer; the `std::string` lives and dies inside the shim | `TestStdStringAndVector` | the shim and the library must use the same standard library and the same `_GLIBCXX_USE_CXX11_ABI`, the preprocessor macro that selects between libstdc++'s two incompatible `std::string` implementations |
| 3 | `std::vector<T>` | copied in and out through the caller's slice | `TestStdStringAndVector/std::vector_in_and_out` | same as `std::string` |
| 3 | `std::string_view`, `std::span` | borrowed: `(pointer, length)`, no copy, valid for the call | **designed, not measured** (derived from the v3 shim's behaviour, commit `5b4fef7`, which is not on `main`; the shim receives the raw pair and constructs the view) | the callee must not retain the view |
| 3 | `std::array<T,N>` | by value through the shim | **designed, not measured** | same ABI rule as any by-value aggregate |
| 3 | `std::optional<T>` | as a parameter `*T` (nil is `nullopt`); as a return `(T, bool)`, or `(T, error)` with `absent:`; the shim constructs and reads it | **designed, not measured** | the two axes are not the same type, and the return axis follows § *Errors from C++ to Go* |
| 3 | `std::unique_ptr<T>` | a parameter consumes the Go value, a result is owning (`Close` destroys it) | **designed, not measured** | the deleter must be the default one |
| 3 | `std::shared_ptr<T>` | a parameter shares (the reference count is copied), a result is owning | **designed, not measured** | a `shared_ptr` the *Go side* must construct out of nothing needs a control block only C++ can allocate: that is a shim entry point, not a layout trick |
| 4 | POD above 16 bytes / MEMORY class by value | copied through the shim; an sret result — a result returned through a hidden pointer the caller supplies — goes into Go storage (`*static_cast<::err::Big *>(cppgo_result) = ::err::make()`) | `TestByValueRecordShape`, `TestByValueRecordShimCompiles` (g++ accepts the shim) | — |
| 4 | more than 6 INTEGER or 8 SSE arguments | one pointer to a block of word fields replaces the input words. The fields keep the declaration's order; a word is 8 bytes, and a `float` is its own 4-byte word; the receiver is a field like any other, and a `std::string` parameter is built from the two words beside it. Go asserts the block's size and every offset with `unsafe.Sizeof`/`unsafe.Offsetof`, C++ with `sizeof`/`offsetof`, so a disagreement does not compile. Out-parameters stay thunk parameters, and a call whose out-parameters plus the block pointer no longer fit in six INTEGER words is refused at generation time rather than mis-passed | `TestArgumentBlockShape`, `TestArgumentBlockRoundTrip`, `TestBuildArgumentBlockRefusals`; `TestTargetsByValueRoundTrip` (the `Eigen::IOFormat` constructor, sixteen INTEGER words) | a direct binding does not exist for this shape |
| 4 | non-POD by value | the record crosses as `*T`: a parameter's wire word is the object's address and the thunk passes the object, so C++ copies it into the call; a result is constructed into Go's pinned storage with placement new; the declared destructor runs **once, at `Close`** (a record that declares none releases the storage and runs nothing); a failed call releases the raw storage without running a destructor over it; a nil argument is refused before the thunk dereferences it; and because the copy runs inside the shim's `try`, the call carries a `(T, error)` channel even when the declaration is `noexcept`; the rule is repeated in the binding's generated doc comment, which says the returned value is Go-owned and that a parameter is copied into the call while the caller keeps ownership | `TestNonPODByValue` (the copy constructor's +1000), `TestNonPODByValueRecordShape`, `TestNonPODByValueRoundTrip`; `TestTargetsByValueRoundTrip` (`cv::Mat` rows, cols and total; `af::array` sum; `Eigen::IOFormat`) | the type's copy constructor and destructor must be visible to the shim, which they are when the header declares them |
| 5 | exceptions | `try`/`catch` in the shim; the message travels in a thread-local string and arrives as a Go `error` | `TestExceptionsBecomeErrors` | **no direct binding may call a function that can throw**: measured, a throw with no C++ frame terminates the process (`TestExceptionFromCIsFatal`, `TestExceptionAfterACallbackIsFatal`, `TestExceptionThroughTheGoCallbackIsFatal`). A library that reports failures as `noexcept` + status needs no shim |
| 6 | templates | the shim instantiates the specialisation the Go side asked for and calls it | `TestTemplateInstantiation`, `TestTwoPhaseGenerics` | the instantiation must be named: the Go generic API's instantiations are collected and the shim is emitted from that list; a Go instantiation with no C++ counterpart fails at load with the exact `template ...;` line to add |
| 7 | inheritance / `As<Base>` | the shim's cast; `As<Derived>` is a `dynamic_cast` and reports `(*Derived, bool)` | `TestMultipleInheritanceAdjustment` | a `dynamic_cast` requires RTTI — the run-time type information it reads — so a library built with `-fno-rtti` reaches the shim only through `static_cast` |

A **reference to a mirrored record** (`const Slot &` where the record is a Go struct) is a different
shape from the rows above: it crosses as a plain pointer into the C++ subobject, with the right layout
and no reference held, as `nested::Box<int>::slot()` in
[`internal/gen/testdata/nested/nested.hpp`](internal/gen/testdata/nested/nested.hpp) does. Whether that
pointer keeps its receiver alive, or is valid only while the receiver is open, is **not decided yet**
([issue #115](https://git.manu343726.com/manu343726/cppgo/issues/115)); this design states no lifetime
rule for it until that decision is recorded.

## Callbacks

A *callback* — a Go function handed to C++, either as a function pointer or wrapped into a
`std::function` by the shim — is a **slot in a table of five-byte `CALL` instructions**
([`internal/callback`](internal/callback)). Five bytes is the length of an x86-64 relative `CALL`,
which is why the slots have fixed, computable addresses. The instruction a C caller lands on names
the slot. The trampoline saves the fourteen argument registers and calls `runtime.cgocallback`
through a copy of the runtime's host-ABI entry: the code the runtime itself uses to start executing
Go from a foreign thread. The runtime then runs the Go function on the goroutine that made the call,
locked to its `M` (its operating-system thread), with the C frames left where they are.

The ABI, exactly:

- arguments in `RDI, RSI, RDX, RCX, R8, R9` and `XMM0..XMM7`, the register file only. A callback that
  takes a stack-passed argument is a generation error naming the sixth argument and the fix (take a
  pointer to a struct).
- the result is `RAX` or `XMM0`, one register: `Args.Ret` and `Args.RetF`.
- the Go side marshals the frame itself — that is, it copies the argument values in and the result
  out, with no reflection: `func(*callback.Args)` reads `I` and `F`, writes `Ret` or `RetF`.

Lifetime rules:

- **Registration** returns an address in a 64-slot process-wide table, and that table is the runtime's
  only global state. It has to be: C++ calls the address it was given, from any thread, with no
  context that could identify the registration, so the address itself has to carry the identity.
  Registering a function twice gives two independent slots, because C++ may release one while the
  other is still held.
- **Release** waits for the calls already inside the Go function to return. A caller that is done with
  a callback can release it and then free what it captured. It cannot wait for C++: a C++ object
  that still holds the address and calls it after `Release` reaches the dispatcher's released-slot
  path, which reports through `TakePanic` and returns a zero result, a programmer error on the C++
  side, never a use-after-free.
- the callback's Go frame is as valid as the foreign call: it may allocate, grow its stack and take a
  traceback. That is measured; the goroutine id and the thread id are the caller's, and
  `runtime.Stack` works inside it.

**Panic.** A panic inside a callback never unwinds into C++: the dispatcher recovers it, zeroes the
result, records a `*callback.Panic` (value plus stack), and the binding that passed the callback
checks `callback.TakePanic()` when the foreign call returns and re-panics there. Measured by
`TestCallbackPanicIsCaptured` and `TestCallbackPanicReachesTheCaller`.

**Reentrancy.** Go → C++ → Go → C++ nests. The runtime counts foreign calls per M, and a callback may
make another reentrant call. `TestCallbackReentrancy` measures two levels.

**Exceptions.** A C++ exception thrown *by* a callback falls outside the ABI. A callback
that wants to report a failure writes a status into `Args.Ret` and the C++ side checks it. An
exception raised across a Go callback frame terminates the process (measured). The rule is the same as
everywhere else. The C++ side catches, the Go side reads a status.

## The `std::` ABI

`std::` types cross at the shim, and the shim is C++ compiled by cppgo. The dependency is therefore
explicit and checkable rather than a layout trick:

- the shim is compiled with the flags the generator discovered for the header, which is how a
  `_GLIBCXX_USE_CXX11_ABI` value the library was built with reaches the shim. The compiler is the one
  `-cxx` names, else `g++`, else `clang++`, and it is named in the report. What the generator does
  **not** do yet is detect the library's own standard library (`libstdc++` vs `libc++`) and refuse a
  shim that disagrees with it: matching the compiler family and the standard library is the user's
  input today, and a mismatch surfaces when the shim is linked or loaded, not as a generation error.
- `std::string`'s layout never appears in Go. It appears in the shim. The shim is where a
  `std::string` can legally be constructed.
- a `std::` type outside the table still crosses as an opaque value, constructed and destroyed by the
  shim.
- one case is worth naming, because it is the only place a layout *is* used: a `std::function` that
  the Go side must build without a C++ factory. It is 32 bytes on libstdc++ (`_Any_data` 16,
  `_M_manager` 8, `_M_invoke` 8), and the two code pointers can only come from instantiations the
  library or the shim exports. v4 does not need it; the shim constructs the `std::function` from a
  function pointer, as `TestCallbackThroughTheShim` measures, and the layout stays recorded in
  [`internal/callback`](internal/callback)'s `TestStdFunctionFromGo` as the fallback for a library
  that already exports those instantiations.

## Errors from C++ to Go

A C++ library reports failure in at least eight ways, and a Go caller should not be able to tell which
one it used: **every fallible call returns `(T, error)`, the value first, the error last, and every
infallible call returns no error at all.** `std::error_code` is the standard library's failure value,
a code plus the category that interprets it; `std::expected<T,E>` is C++23's value-or-error type, and
`tl::expected` is the widely used pre-C++23 library with the same shape. The mechanism that reads them
lives in the shim (which is C++ and can therefore catch, read an `error_code` and construct an
`expected`), the projection lives in the generated Go package, and
[`internal/errormap`](internal/errormap) is the part of it that is shared and measured.

### The mapping

| C++ convention | how it is recognised | shim does | Go signature and error |
|---|---|---|---|
| **exceptions**: `std::exception`, a user subclass, `std::system_error` | the declaration is not `noexcept` | `try`/`catch` around the call, and around the marshalling; writes the message, and for `std::system_error` its code and category | `(T, error)`; `*cppgo.CallError` wrapping `*errormap.Message` (text only) or `*errormap.ErrorCode` (code, category, message) |
| **`std::error_code`** | the return type is `std::error_code` | calls, tests the code, writes code + category + message | `(T, error)`; `*errormap.ErrorCode`; `errors.Is` compares code **and** category |
| **`std::expected<T,E>`**, `tl::expected<T,E>` | the return type has `has_value()`/`value()`/`error()`, or its spelling is `std::expected` | tests `has_value()`, copies the value out, projects `E` | `(T, error)`; `E = std::error_code` → `*errormap.ErrorCode`; `E = std::string` → `*errormap.Message`; a custom `E` → the type the config names |
| **`std::optional<T>`** — **designed, not measured** | the return type is `std::optional<T>` | copies the value, or writes "empty" | **not an error by default**: `(T, bool)` — `optional` means "absent", and a value that is absent is not a failure. A package that wants it as an error writes `kind: optional` with `absent: "<message>"`, and then it is `(T, error)` |
| **status int** — **designed, not measured** | a plain integer return, only with a config entry | returns the int, the projection maps it | `(T, error)`; `*errormap.ErrorCode` with the category the config names (default `"status"`) |
| **`errno`** — **designed, not measured** | a config entry (`kind: errno`) | reads `errno` after the call | `(T, error)`; `*errormap.ErrorCode` with category `"errno"` |
| **`bool` + out parameters** (`bool parse(const char*, Result*, Error*)`) | **never inferred** — a `bool` says nothing | the config names the value channel and the error channel; the shim writes both | `(T, error)`; the `bool` does not appear in the Go signature |
| **a custom result type** (`outcome<T>` and `absl::StatusOr<T>`, two widely used third-party result types, or a project `Result<T>`) | the shape: `has_value()`, `value()`, `error()` | tests the shape and projects the error member | `(T, error)`; the error member is projected as above, or by the type the config names |
| **`std::variant<T,E>`** with a known error alternative — **designed, not measured** | **never guessed**: a config entry names it with `alternative: <index>` | tests `index()`, copies the matching alternative | `(T, error)`; the config names which alternative is the error and how to project it |

Two rules hold across the table, and they are what make the generated API predictable:

- **no error is synthesized for a call that cannot fail.** A `noexcept` function returning `int` is
  `func(...) int32`. The generated package has no `error` that is always nil.
  `TestNonFallibleCallHasNoErrorResult` pins it.
- **no failure is dropped.** A convention that is neither inferred nor configured is a **generation
  error**: the report names the declaration and the config entry it needs, and nothing is emitted for
  it. Silence would mean a call whose failure becomes a zero value.

**Exceptions during marshalling use the same path.** Constructing a `std::string`, a `std::vector` or
any by-value object can throw `std::bad_alloc`. The shim's `try` covers the marshalling as well as the
call, so a failure before the call is an error like any other.

**There is no exception support without a C++ frame**, and v4 says so plainly. A direct binding is
`noexcept` by construction, and a throw with no C++ frame above it terminates the process (measured,
three times, in `internal/callback`'s child-process tests). A library whose API throws and whose
headers cannot be included by the shim cannot be bound safely. The fix is a `noexcept` entry point in
the library.

### The generated Go error types

```go
// ErrorCode is a std::error_code, a status int or an errno projected into Go.
// errors.Is compares Code and Category; the fields are exported, so a caller
// never has to parse the string.
type ErrorCode struct {
	Code     int
	Category string
	Message  string
}

// Message is a failure that carries only text: a thrown std::runtime_error, or
// a std::expected whose error alternative is a string.
type Message struct {
	Text string
}
```

- the binding returns `*cppgo.CallError{Symbol, Err}` with one of these underneath, so `errors.As`
  finds both the declaration and the projection, and `errors.Is` finds the failure's identity (a
  `std::error_code`'s code and category are its identity, and a message's text is its own).
- a custom `E` projects to the Go type the config's `type:` names, generated with that C++ type's
  accessors. The generated doc comment states whether `errors.Is` and `errors.As` are supported for
  it: a projection with no comparable identity says `errors.Is` does not apply rather than pretending.
- the per-symbol report names the mechanism and whether it was inferred or configured. That is the
  line a reader uses to audit a generated package: one line per declaration, carrying the C++
  symbol, the mechanism, `inferred` or `configured`, and the channels the value and the failure
  travel through.

```
ns::open_file      error_code  inferred     value=return error=return
ns::parse_int      expected    inferred     value=return error=return
ns::parse_point    out_param   configured   value=1 error=2
ns::divide         exception   inferred     value=return error=return
ns::twice          no error channel         inferred
```

### The configuration

For the conventions a signature cannot state, a package writes a `cppgo.yaml` beside the generated
package — the file the planned `-errors` flag names (§ *Generation. Input, selection, evidence,
output*) — or writes the same data as a header annotation. Its `errors:` map is keyed by qualified
C++ symbol:

```yaml
errors:
  "ns::parse":
    kind: out_param      # exception | error_code | expected | optional | status | errno |
                         # out_param | custom | variant
    value: return        # where the result is: "return", a parameter index, or a parameter name
    error: 1             # where the failure is: "return", a parameter index, or a parameter name
    type: ns::ParseError # optional projection: the C++ type of the error

  "ns::find":
    kind: optional
    absent: "not found"  # an empty optional becomes this error; omit it and the call is (T, bool)

  "ns::resolve":
    kind: variant
    alternative: 1       # which std::variant alternative carries the error; never guessed
```

Every key above exists in `errormap.Entry`, and **the loader rejects an unknown key** rather than
ignoring it: `valuee:` or `err:` is a parse error naming the field, because a mistyped channel would
otherwise leave an entry that names nothing.

Rules, each of which is a test in `internal/errormap` (`TestConfigValidation`):

- **validated against the declaration at generation time**: a channel that names no parameter, a
  parameter that cannot carry a channel (a value parameter is not a pointer or a reference), an entry
  for a symbol the package does not bind, a kind that returns its own error being given channels, an
  `out_param` entry without both channels, and `absent` or `alternative` on a kind that does not use
  them, each is a generation error, and **no output is written**.
- **no failure is dropped**: a declaration that can fail and whose convention is neither inferred nor
  configured is an error naming the declaration and the kind to add (`TestFallibleWithoutConventionIsAnError`).
- **inferred cases need no entry**, and an entry for one may only add a projection.
- **a config names channels inside an existing declaration.** It adds no signature.
- the file is part of the generated package's inputs, so `cppgo check` fails when it changes without a
  regeneration.

Evidence for the mechanism, end to end, is `internal/errormap`: a user library and the cppgo-built
shim built as two objects, and six C++ functions covering **five** of the table's rows, a
`std::error_code` return, a `std::expected<T, std::error_code>` (C++23), a `std::runtime_error`, a
`std::system_error`, a `bool` + out-parameter function driven by
[`internal/errormap/testdata/cppgo.yaml`](internal/errormap/testdata/cppgo.yaml), and a
`ns::Result<T>` inferred from its shape, each asserted with `errors.As` and `errors.Is` on the Go side
(`TestErrorCodeReturn`, `TestExpectedReturn`, `TestExceptionReturn`, `TestConfiguredOutParamReturn`,
`TestInferredCustomResult`), plus every validation error above.

**Four rows of the table above carry `designed, not measured` in their first cell**, `std::optional`
as an error, a status int, `errno`, and a `std::variant` alternative. Their rules and their validation
are implemented and tested (`TestOptionalAndVariantRules`), and no binding exercises them end to end
yet. That is the emitter's work, and the mark is on the row so a reader need not hold six
functions against nine rows.

**Every block a thunk writes into is Go storage pinned for the call.** A stack local's address is a
`uintptr` the moment it is handed to the C++ side, and a Go frame moves. The message the projection
reads through that address would be a view of memory the frame no longer owns. The projection also
copies each string out of the block, so an `error` never keeps a view of storage the next call
reuses. `internal/errormap`'s bindings do both, and a test caught the omission rather than a review.

*The walkthrough, what a user writes, what the generator reports, and how to migrate a library, is
the tutorial, [issue #5](https://git.manu343726.com/manu343726/cppgo/issues/5). This section is the
mechanism the tutorial documents.*

## Templates and the two-phase generics flow

A C++ template only exists where it is instantiated: the compiler emits a function template's body
only for the concrete arguments something uses it with, `maxof<int>` and not `maxof` in general. For a
template-heavy library — Eigen, the C++ linear-algebra template library, above all — the Go API is
generic and the C++ instantiation is a build step. That is the *two-phase flow*: `cppgo gen` writes
the generic API, then a second phase adds the C++ instantiations the Go program actually uses. The
second phase is the `cppgo build` command, which names the Go packages to read with `-scan` and the
directory their patterns resolve against with `-scan-dir`. It takes four steps:

1. **the Go program instantiates the generic API** — `Matrix[float64, 4, 4]`,
   `Vector[float32, 3]`. The user writes only the Go program.
2. **cppgo collects the instantiations** — `cppgo build -scan ./cmd/...` type-checks the Go program
   with `go/packages` and reads the type arguments at each instantiation, or takes an explicit list
   the package declares (the `-instantiate` manifest entries, for a program that wants to pin the
   set). The output is a list of C++ specialisations: `Eigen::Matrix<double,4,4>`,
   `Eigen::Vector3f`.
3. **cppgo emits the shim from that list** — `template struct Eigen::Matrix<double,4,4>;` plus a thunk
   per operation the Go type uses, and **compiles it** into the cppgo artefact that links the
   library.
4. the generated Go package calls the thunks. The failure mode when a Go instantiation has no C++
   counterpart is a load-time error naming the exact line to add: `TestTwoPhaseGenerics` runs a
   third program that asks for `Box[int64]`, which the shim was not built for, and asserts the error
   carries `template struct ::box::Box<std::int64_t>;`.

What each target exercises, and what is out of scope with the reason. They are three real C++
libraries, each with a different shape of problem: **Eigen** is linear algebra over templates,
**OpenCV** is computer vision with a header of hundreds of overloads, and **ArrayFire** is GPU array
work behind a C++ API of its own.

| target | what it exercises | out of scope, and why |
|---|---|---|
| **Eigen** | the two-phase flow above; a header-only library with no `.so`, so the shim is the whole binary; expression templates — the C++ technique where an arithmetic expression builds a small tree of types that is evaluated later, in one loop — which are compiled in the shim and never appear in a Go signature; POD by value (`Matrix4d` is 128 bytes, MEMORY class, copied by the shim) | Eigen's expression templates across the Go boundary: a Go signature names `Matrix[T,R,C]`, never `CwiseBinaryOp<...>`; the shim materialises the expression with `.eval()` |
| **OpenCV** | whole-header binding at scale: hundreds of overloads, default arguments, `cv::Mat` as an opaque value with `Close`, `std::string` and `std::vector<T>` everywhere, enums with explicit values | GPU modules that need a CUDA runtime at link time: the shim links what the library links, and a missing device is the library's error, not a binding failure |
| **ArrayFire** | C++ API over a C API; handles are opaque values; `af::array` by reference; exceptions are `af::exception`, caught by the shim and reported as Go errors | none measured yet; the exception row is the one to verify first, because ArrayFire throws by design |

**Qt is out of scope.** No slice, no tag, no CI package, and nothing in this contract is designed
against it.

### The target-slice test harness

Each target is delivered as **a generated binding plus a test against the real library**, never a
fixture, never a mock, and never a hand-written declaration. A slice is accepted when the test compiles
its generated package against the real headers, loads the real shared object through the cppgo-built
shim, and asserts on real values.

Layout, one directory per target. None of them exists yet; they are created with the slices.

```
internal/targets/opencv/     generated package (zz_cppgo.gen.go, zz_cppgo_amd64.s) + opencv_test.go
internal/targets/arrayfire/  the same, against libaf
internal/targets/eigen/      the same, header-only: the shim is the whole binary
```

Tags and skipping:

- each test file carries its own build tag, `//go:build cppgo_opencv`, `//go:build cppgo_arrayfire`,
  `//go:build cppgo_eigen`, so `go test ./...` on a machine without the library neither compiles the
  test nor fails.
- inside the tag, the test **skips with the reason** when the library is absent: `pkg-config --exists
  opencv4` for OpenCV, `pkg-config --exists af` (or `af/version.h`) for ArrayFire, and
  `<eigen3/Eigen/Dense>` for Eigen. A skip is a skip. The message names the package
  to install (`libopencv-dev`, `libarrayfire-dev`, `libeigen3-dev`).
- **the ArrayFire slice is never deleted**: if it cannot be installed on the CI image, its tag stays
  and the opt-in host is documented in the test's own comment and in the target's page. The contract
  forbids removing a slice because its dependency was awkward once.

The CI image ([issue #13](https://git.manu343726.com/manu343726/cppgo/issues/13) and
[#16](https://git.manu343726.com/manu343726/cppgo/issues/16)) installs the three packages. The
default job runs the slices with their tags on. A job that cannot install one runs the rest and
reports the skip. The harness is the only place the tags are turned on, so a developer's
`go test ./...` stays fast.

What each slice asserts:

| slice | generated API exercised | assertions |
|---|---|---|
| **OpenCV** | `Mat`, `cvtColor`, `imwrite`/`imread` through the whole-header binding | create a `Mat`, fill it, convert to grey and check `Channels()`, `Rows()`, `Cols()`, `Type()`; write it and read it back with the same shape |
| **ArrayFire** | `af::array`, `sum`, `matmul`, `fft`, `host` | construct an array from a Go slice, `sum` over an axis, `matmul` of two small matrices against a hand-computed product, `fft` of a constant against the known spectrum, and read the values back through `host` |
| **Eigen** | the generic `Matrix[T,R,C]` and `Vector[T,N]` API, fixed and dynamic | add, multiply, transpose, inverse and norm with numeric assertions the test computes itself, plus element access at both dimensions; a fixed `Matrix[float64,4,4]` and a dynamic `Matrix[float64,Dynamic,Dynamic]` exercise the two-phase instantiation flow |

**Only the generated API.** No `cppgo.Handle` in a test, no hand-written binding, no C++ file written
by the test author. If a slice needs one of those, the generator is missing a construct and that is
the finding. The error rows of such a library (OpenCV's `cv::Exception`, ArrayFire's
`af::exception`) are asserted as Go errors through the mapping above. That is what makes the slices
the acceptance test for both halves of v4 at once.

The slices are **not implemented yet**: this section is the contract and the test plan they are
built against.

## Whole-header API at scale

A header is the unit of selection. The generator has to answer the questions a large header asks:

- **Overloads** take a suffix built from the Go parameter types, in order (`Dot`, `DotFloat64Slice`,
  `DotVector`). One declaration keeps the bare name. A collision that no suffix separates is a
  generation error naming both declarations and asking for `-rename`.
- **Default arguments** produce one Go function per prefix of the parameter list that the defaults
  allow, generated only for the prefixes actually used (`f(a)`, `f(a,b)`), never a variadic soup. A
  prefix that would collide is dropped and the report says so.
- **Namespaces** leave the Go name unchanged. Two declarations from different namespaces that
  map to one identifier need `-rename`, and without one the error names both.
- **Enums** become named Go integer types with typed constants, scoped and unscoped alike, with the
  underlying type from libclang.
- **Macros** are not parsed: libclang sees a macro's expansion, not its declaration, so a macro that
  *is* the API (glibc's `__attribute__` wrappers, OpenCV's `CV_EXPORTS_W`) is invisible, and the
  underlying declaration is bound instead. A macro-only API — a function-like macro that expands to a
  call — cannot be bound at all, and the report names the macro it could not see; the fix is a
  function, which every large library provides.
- **Classes with inline bodies, templates, and `std::` members** need no work from the user. They are
  shim work.
- **What is emitted** is every public declaration in the headers, plus the type closure of every bound
  signature. `-bind` and `-exclude` narrow it. An unmatched entry is an error, so a typo cannot
  silently shrink the API.

## Type map

Sizes are validated against libclang's `sizeof` for the translation unit. A platform where `long` is
not 64-bit is a generation error, not a silent reinterpretation.

| C++ | Go | notes |
|---|---|---|
| `bool` | `bool` | |
| `char`, `signed char` | `int8` | plain `char` is signed on the supported targets |
| `unsigned char` | `uint8` | |
| `short`, `unsigned short` | `int16`, `uint16` | |
| `int`, `unsigned int` | `int32`, `uint32` | |
| `long`, `long long`, `unsigned long`, `unsigned long long` | `int64`, `uint64` | LP64 — the model in which `long` and a pointer are 64 bits — validated |
| `int8_t`…`uint64_t` | the same width | fixed-width aliases resolve by size |
| `size_t`, `uintptr_t`, `intptr_t`, `ptrdiff_t` | `uint64`, `int64` | validated |
| `float`, `double` | `float32`, `float64` | |
| `wchar_t`, `char16_t`, `char32_t` | `int32`, `uint16`, `uint32` | validated by size |
| enum (scoped or unscoped) | named integer type + constants | underlying type from libclang |
| `T *`, `T &` | `*T` | borrowed; `[]T` for the pair form |
| `(T *p, size_t n)` | `[]T` | borrowed; `const` decides whether the callee may write |
| `(char *p, size_t n)` | `string` | borrowed for the call; not NUL-terminated |
| mirrorable record, by value | `T` | the Go struct *is* the C layout |
| non-POD record, by value | `*T` | a parameter's word is the object's address and the shim passes it, so C++ copies it into the call; a result is placement-new'ed into Go's pinned storage, where the destructor the record **declares** runs once, at `Close` — a record that declares none releases the storage and runs nothing (`TestNonPODByValueRecordShape` asserts both sentences); the call returns `(T, error)` whatever the declaration promises |
| `T[N]` (record field) | `[N]T` | fixed-size array |
| `std::string` | `string` | copied in and out |
| `std::string_view` | `string` | borrowed for the call; **designed, not measured** |
| `std::vector<T>` | `[]T` | copied in and out (measured) |
| `std::span<T>` | `[]T` | borrowed; **designed, not measured** |
| `std::array<T,N>` | `[N]T` | by value; **designed, not measured** |
| `std::optional<T>` as a **parameter** | `*T` | nil is `nullopt`, a non-nil pointer is the value; **designed, not measured** |
| `std::optional<T>` as a **return** | `(T, bool)` | absence is not a failure; a config entry with `absent:` makes it `(T, error)`; **designed, not measured** |
| `std::unique_ptr<T>` | `*T` | a parameter consumes, a result owns; **designed, not measured** |
| `std::shared_ptr<T>` | `*T` | a parameter shares, a result owns; **designed, not measured, not supported yet** |
| function pointer | `func(...)` | the callback table's address (measured) |
| `std::function<R(Args...)>` | `func(...)` | the shim constructs the object (measured) |

`long double` and `__int128` have no Go value type. A signature that uses one is a generation error
naming the parameter and the fix (widen/narrow at the C++ boundary to `double`/`int64`, or wrap it).

A pointer parameter followed by a length parameter of integer type becomes a slice. The pointer is the
data, the length is `len(slice)`, and the slice is borrowed for the call. One length may serve several
pointers. The generated binding checks before the call that every slice sharing a length has the same
length.

## Naming and overloads

- A name is CamelCase, split on `_`, leading and trailing underscores stripped.
- `New<Type>` for a constructor, `Close` for the destructor, `<Type><Name>` for a static member.
- Constructor overloads follow the same suffix rule as functions, applied to `New<Type>`. A copy
  constructor is `Clone()`, and a move constructor stays internal.
- A **class template specialisation** is `<TemplateName><Arguments>`: `Box<int>` → `BoxInt`,
  `Matrix<double,4,4>` → `MatrixFloat64FourFour` in the concrete API and `Matrix[float64,4,4]` in the
  generic one.
- A **conversion operator** is named after its target Go type (`Int()`, `Float64()`, `String()`,
  `Bool()`, `AsVector`).
- **Operators take a fixed name** (`Add`, `Equal`, `At`, `WriteTo`, …). Anything not in the table, or a
  collision, is a generation error asking for `-rename '<qualified signature>=<GoName>'`. `operator
  new`/`operator delete` are deliberately absent, because the Go side allocates.
- A **static data member** is a package function `<Type><Field>()`, plus `<Type>Set<Field>(v)` when it
  is mutable.
- `Open` is reserved as the package entry point. A `-rename` that matches nothing is an error.

## Errors

The runtime and the generated package report through a small set of typed errors, so `errors.As`
finds the layer that failed:

- `*OpenError` — the library could not be loaded; it carries the path and the loader's error.
- `*SymbolError` — a symbol was not found; it carries the name and the error.
- `*NativeError` — a failure the foreign library reported, whose message is its text verbatim.
- `*CallError` — one binding's call failed; it names the C++ declaration and wraps the native error.
- `*callback.Panic` — a panic raised inside a callback, captured rather than unwound into C++.
- `ErrClosed`, a sentinel — the receiver has no live object: a nil pointer, or one already closed.

The embedded path of § *Loading* adds four more, each naming the file and the cache directory
involved: `*ExtractError` (an object that could not be written out), `*DependencyError` (a
`DT_NEEDED` entry that neither the embedded set nor the loader provides), `*SearchError` (a name no
search location held, listing the paths that were tried) and `ErrInsecureCache` (a cache path that is
not a private directory, so extraction is refused). [ADR-0003](docs/adr/0003-embedded-libraries.md)
owns that mechanism.

The `-status` convention remains available for C++ APIs that report failures as a 32-bit return code.
The Go binding returns `error` instead of the integer, and wraps the message in `*CallError`.

## The ABI the direct path calls

This is the section the term *ABI* at the top of the file points at. A call compiled for x86-64 Linux
follows the System V AMD64 calling convention, as the Itanium C++ ABI extends it with rules for
objects; the generator validates a signature against these rules, and the trampolines implement them:

- arguments travel in registers while registers are left: the first six integer and pointer
  arguments in `RDI, RSI, RDX, RCX, R8, R9`, and the first eight floating-point arguments in
  `XMM0…XMM7`. The two files are independent, so an `int` and a `double` each take the first slot of
  their own file.
- a hidden *sret* pointer takes the first integer slot when the result is returned through memory.
  `this` — the object a member function is called on — takes the next slot. The declared arguments
  follow.
- results: `RAX` for an integer or a pointer, `XMM0` for a float, and up to two registers for a POD of
  at most 16 bytes.
- a slice or string parameter is `(pointer, length)`, pointer first, the two adjacent in the integer
  file.
- the trampoline spills no argument register: it has exactly the slots the call was given. More than
  6 integer or 8 SSE slots, or a by-value aggregate the ABI puts in memory instead of registers, is a
  shim binding. **Results** are different: an sret result is written through the hidden pointer, which
  the caller supplies and which can therefore point into Go storage.

The reentrant carrier has one extra rule, measured the hard way. `runtime.cgocall` reaches the
register-loading wrapper by a tail jump: no return address is pushed, so the wrapper starts with `RSP`
16-byte aligned, where an ordinary call target starts 8 bytes off. The wrapper's own `CALL` must
therefore not compensate for a return address it never received. A callee compiled with aligned SSE
stores faults immediately if this is wrong. `TestCallReentrantGivesTheCalleeAnAlignedStack` is the
regression test.

## The trampoline

Measured 2026-11-03, Go 1.27, on an 8-core x86-64 CPU pinned to 964 MHz with boost disabled. At that
clock the absolute nanoseconds are inflated by roughly 4.5×, so the cycles and the ratios are the
honest numbers:

| path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| **cppgo trampoline, direct** | **13.9** | **13** |
| cppgo trampoline, stack switch | 14.9 | 14 |

Measured again 2026-11-08 on the same machine with the carriers side by side
(`go test -bench . -benchtime 200000x`): direct 15.9 ns/op, reentrant 151 ns/op (one 16-byte
allocation per call), callback round trip 481 ns/op. A shim thunk adds one C++ call frame inside the
library. The Go-side cost is the same trampoline.

One generated trampoline — the one for a member function that takes no argument and returns a
`double`. It is the emitter's output, with one explanatory comment added on the alignment line:

```asm
TEXT ·callNorm(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVQ	SP, R13
	ANDQ	$~15, SP        // SysV: RSP 16-aligned *before* the CALL
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+16(FP)
	RET
```

Every line of it, for a reader who has not written Go assembly before. `TEXT ·callNorm(SB), NOSPLIT,
$0-24` declares the function: `·callNorm` is its Go name (the `·` is the assembler's separator between
package and name), `SB` — the *static base* — is how this dialect spells a symbol, `NOSPLIT` removes
the stack-growth preamble, and `$0-24` says the function has a zero-byte local frame and 24 bytes of
arguments and results. `FP` is a pseudo-register the assembler synthesises to name an argument slot,
so `fn+0(FP)` is the argument at offset 0, `this+8(FP)` the one at offset 8, and `ret+16(FP)` the
result at offset 16. `SP` is the real stack pointer.

- `MOVQ fn+0(FP), R12` copies the foreign function's address into `R12`, a register the call does not
  use for arguments.
- `MOVQ this+8(FP), DI` loads the object pointer into the first integer argument register.
- `MOVQ SP, R13` saves the goroutine's stack pointer; `ANDQ $~15, SP` then rounds the stack pointer
  down to a 16-byte boundary. `$~15` is the immediate whose low four bits are clear, so the `ANDQ`
  clears the low four bits of `SP`. Rounding down and not up keeps the value inside the frame the
  runtime gave the function.
- `CALL R12` is the foreign call itself. Because `SP` was 16-byte aligned before it, the callee finds
  the 8-mod-16 alignment it expects at its entry.
- `MOVQ R13, SP` restores the goroutine's stack pointer.
- `MOVSD X0, ret+16(FP)` stores the floating-point result — the callee leaves it in `XMM0`, which this
  dialect names `X0` — into the caller's result slot.
- `RET` returns to Go.

Rules the contract fixes:

- arguments are read **before** SP is switched and results are stored **after** it is restored.
- `NOSPLIT`, zero frame, no allocation from the trampoline itself, no Go memory touched.
- `ProbeStack()` runs before the trampoline, **from the same goroutine and before any pointer into the
  caller's frame is taken**: the probe grows the stack, which moves that frame. This is measured, not
  theoretical: it is what broke `internal/shim`'s first draft, where the probe ran after the out
  pointers were formed, and it is why a `t.Run` subtest (a new goroutine) probes again.
- correctness evidence, required on every run: a NOSPLIT asm function is never asynchronously
  preempted; GC torture with `GOGC=1` under 32 goroutines; `SIGPROF`/`SIGURG` torture (`SIGPROF` is
  the signal the runtime profiles with and `SIGURG` the one it preempts with, so delivering them
  during a foreign call is what the torture measures); `-race`; and `GOMAXPROCS=1`.

## Loading

[`github.com/ebitengine/purego`](https://github.com/ebitengine/purego) is the only third-party runtime
dependency, and this is deliberate: it solves `dlopen`/`dlsym` without cgo by importing its own
`internal/fakecgo` package, a stand-in for the cgo runtime. That package is larger and less forgiving
than any loader we would write.

Two entry points hand the process a library: `Open` takes a path, and `OpenFS` first extracts an
embedded file set into a private per-user cache and loads the object from there
([ADR-0003](docs/adr/0003-embedded-libraries.md)). Above them, the `CPPGO_LIBRARY_PATH` environment
variable and the generated package's `CppgoLibraryPath` variable override where a name is looked for.

We use only purego's `Dlopen` and `Dlsym`, never its call path — the machinery it offers for invoking
a foreign function, which cppgo does not need because it has trampolines of its own. One dependency
has to be stated plainly. The reentrant carrier calls `runtime.cgocall`, and that function throws
`cgocall unavailable` unless the cgo runtime has been initialised. `runtime/cgo` does it under
`CGO_ENABLED=1`, and purego's fakecgo does it under `CGO_ENABLED=0`. A program that uses a
callback-capable binding without either would fault at the first call. Importing the cppgo runtime
package pulls purego in, so every generated package satisfies this by construction, and the runtime's
own test asserts it.

## Generation. Input, selection, evidence, output

The three commands — `cppgo gen`, `cppgo build` and `cppgo check` — share one flag set, which is
the list below; `gen` ignores `-scan`, because the instantiations are phase two's input.

- **Input.** `-header` (repeatable), `-package`, `-out`, `-library`, `-cxx` (the C++ compiler for the
  shim, defaulting to `g++`, then `clang++`), `-compile-commands`, `-bind`, `-exclude`, `-instantiate`,
  `-rename` and `-status`; `-scan` and `-scan-dir`, phase two's input; `-check` and `-q`, which decide
  what the run does with its result. **Planned for a later `cppgo gen`**: `-errors cppgo.yaml`, the
  package's error configuration. When the emitter debt
  above is paid it becomes a generation input like the headers, so `cppgo check` fails when it changes
  without a regeneration. Until then the only code that reads it is `internal/errormap`'s tests.
- **Compilation flags** come from `compile_commands.json` and `.clangd`, in clangd's order, and are
  never invented silently: when neither names the header, the run assumes `-std=c++17` and the
  header's own directory and says so with a warning, because a parse without the project's real flags
  can misread the header. A database entry's relative paths are rebased against the entry's
  `directory` — the
  include search paths (`-I`, `-isystem`, `-iquote`, `-idirafter`) and the included files
  (`-include`, `-imacros`), in both the joined and the separated spelling. clangd does this, and
  libclang does not: it resolves a relative path against cppgo's working directory instead. That is a
  different tree, it fails silently, the parse cannot see the header's declarations, and a
  declaration that resolves some other way is what gets bound. That is v3's *phantom binding*: on the
  `dyn` fixture, a relative `-Iinclude` produced `dyn::offset(int&, double, double)` while the real
  `dyn::offset(Point&, …)` was never seen. v4 rebases the paths in `rebaseArgs`, measured by
  `TestRebaseArgs`, `TestDiscoverRebasesDatabaseIncludePaths` and
  `TestProjectSeesTheDeclarationThroughARebasedIncludePath` (the last one fails without the rebase).
- **Evidence.** libclang answers types, sizes, offsets, visibility, inline-vs-out-of-line, virtual,
  exception specification, defaulted/deleted special members, template arguments and base lists. The
  ELF tier is a cross-check for direct bindings against the library's `.dynsym`.
- **Output.** `zz_cppgo.gen.go` (the API), `zz_cppgo_amd64.s` (the trampolines), `cppgo_shim/zz_cppgo_shim.h`
  and `cppgo_shim/zz_cppgo_shim.cpp` (the thunks), and the compiled shim `libcppgo_<package>.so`, all
  under `-out`. The generated Go file carries the exact invocation that produced it.
- **Shim build step.** cppgo compiles the shim itself: the flags discovered for the header, `-fPIC
  -shared`, `-Wl,-rpath,$ORIGIN` so the shim finds its siblings wherever it lands
  ([ADR-0003](docs/adr/0003-embedded-libraries.md)), and the library named as a link input by its
  soname rather than its build path. The compiler is the one `-cxx` names, else `g++`, else
  `clang++`. Matching the library's compiler family and standard library is the user's input: the
  generator does not read either back out of the library (§ *The `std::` ABI*). The report's last line
  names the object and the compiler that built it, `cppgo: built <shim> with <compiler>`. A compile
  error is a generation error carrying the compiler's message and the binding whose thunk it is in,
  never a symbol that fails at the first call.
- **Determinism.** The same inputs produce byte-identical Go and C++. Bindings are sorted by qualified
  name and thunk names come from a stable hash of the qualified signature. `cppgo check` is the CI
  gate, and the golden files under `internal/gen/testdata` are its unit-level equivalent.
- **Comments.** The C++ doc comment becomes the Go doc comment, and every binding carries exactly one
  evidence line: `// C++: <signature>, <mangled> (<carrier>[ <thunk>], <reason>)`, ending
  `, verified against <library>` when the ELF cross-check ran. Real lines, one per carrier, from
  [`benchmarks/cppgo/zz_cppgo.gen.go`](benchmarks/cppgo/zz_cppgo.gen.go):

  ```go
  // C++: bench::Counter::get() const noexcept, _ZNK5bench7Counter3getEv (direct, noexcept, not virtual, register class; the library exports the symbol), verified against libbench.so
  // C++: bench::Counter::Counter(std::int64_t), _ZN5bench7CounterC1El (thunk cppgo_thunk_d33d476a9f5f, not noexcept: an exception would unwind into Go), verified against libbench.so
  // C++: bench::fold(const std::function<std::int32_t (std::int32_t, std::int32_t)> &, std::int32_t, std::int32_t), _ZN5bench4foldERKSt8functionIFiiiEEii (reentrant cppgo_thunk_35edc5cea344, callback: the call enters C through runtime.cgocall, so C++ may call back into Go), verified against libbench.so
  // C++: bench::Counter::Counter(const bench::Counter &), synthesized (thunk cppgo_thunk_568116dd0671, no mangled symbol), verified against libbench.so
  ```

  `synthesized` stands in for the symbol when the declaration has none of its own, as a
  compiler-generated copy constructor does.

## Hard limits, measured

These are the places where the contract says "no", with the measurement behind it:

1. **An exception with no C++ frame terminates the process.** Three child-process tests in
   [`internal/callback`](internal/callback) measure it: from a direct call, after a callback
   returned, and through a Go callback frame. The only support is the shim's `try`/`catch`.
2. **A callback requires the reentrant carrier.** A callback reached through the direct trampoline
   dies with `fatal error: morestack on g0` (`TestCallbackOnTheDirectTrampolineIsFatal`).
3. **A callback carries at most six INTEGER and eight SSE arguments and one register result.** The
   trampoline reads the register file only.
4. **`runtime.cgocall` needs the cgo runtime initialised** (runtime/cgo or purego's fakecgo), stated
   above.
5. **A `std::` layout is only ever in C++.** The one layout the Go side can be forced to use, a
   `std::function` built without a C++ factory, needs the library to export two template
   instantiations. V4 avoids it by having the shim construct the object.
6. **The generator cannot bind a macro that is the API**, because libclang sees expansions.
7. **A template instantiation that no one asked for does not exist.** The two-phase flow makes the
   failure a load-time error naming the line to add.
8. **`long double` and `__int128` have no Go value type.**
9. **The shim and the library must share a C++ ABI and a standard library.** Mixing libstdc++ and
   libc++ breaks the link or the load (§ *The `std::` ABI*). The generator does not detect it yet, so
   this is a constraint on the input rather than a refusal.

## What remains a generation error

Only input that names no Go construct, each with the thing the user supplies to make it
representable:

| input | the error names | what the user supplies |
|---|---|---|
| a template selected as a root with no concrete instantiation | the template and the parameter that is unknown | `-instantiate 'Box<int>'` |
| two bindings that map to one Go name and no suffix separates them | both declarations | `-rename '<qualified signature>=<GoName>'` |
| `long double` / `__int128` in a signature | the parameter or result | a wrapper with a representable type |
| an incomplete type used where a value is needed | the type and the use | include the definition |
| a callback taking more than six INTEGER or eight SSE arguments | the sixth argument | take a pointer to a struct |
| a function-like macro that is the whole API | the macro | a function |
| a shim that does not compile | the compiler's diagnostic, mapped to the binding | fix the header or the flags |

## Not part of this design

- the **user's build compiling generated code**. v3 did this and ADR-0002 reverses it.
- the Go directive language (`DSL`: a small set of code comments that a generator reads) as a
  *primary* mode. `//cppgo:library`, `//cppgo:header`, `//cppgo:class`, `//cppgo:method` and friends
  survive only as the fallback declaration file's syntax.
- a hand-written `extern "C"` shim layer. The shim is generated infrastructure, compiled by cppgo.
- `cppgo.Handle` in a generated API.
- **non-goals: Arm64 and Windows code generation.** The Go package still builds on them; the
  generator does not emit trampolines for an unsupported target, and the error names the target.

## Tests and coverage

- `make test` runs two passes, both required: `CGO_ENABLED=0 go test -count=1 ./...` (the no-cgo
  contract) and `CGO_ENABLED=1 go test -race -count=1 ./...` (the race detector; the toolchain
  rejects `-race` with cgo off). Coverage is collected on the cgo-enabled pass with
  `-covermode=atomic`.
- Table-driven tests with `t.Run` per case, `testify` `require`/`assert`, `t.Parallel()` where cases
  are independent. Concurrency tests run under `-race` and `GOMAXPROCS=1` with at least 32 goroutines.
- Coverage gate: **≥ 99%** on `./...`, measured by `./scripts/coverage-gate.sh`. The gate's
  exclusions are the three it prints, each with its reason: `zz_cppgo*.go` (generator output),
  `internal/gen/testdata/` (generated fixtures), and `internal/testlib/` (test scaffolding). No
  hand-written production package is excluded, and `cmd/` is measured like everything else.
- **Carrier coverage is a test**: every binding in the fixture set is asserted to have a carrier, and
  the golden output is compiled and run for both carriers.
- **The test suite compiles the shim**, which measures "cppgo compiles the shim" against a real
  compiler rather than against a model: [`internal/shim`](internal/shim) builds the user's library and
  the shim as two objects, and asserts the artefact model (no cppgo symbol in the user's library, the
  thunks in the shim).
- **The callback mechanism is measured in both CGO modes**, including the child-process tests whose
  expected outcome is a fatal error.
- The runtime has a test that fails if [`internal/cxx`](internal/cxx) appears in the root package's
  dependency graph. The two layers cannot quietly merge.
