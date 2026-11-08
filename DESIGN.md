# cppgo — design contract (v4)

This file is the contract for every workstream. If something is not written here, do not invent it:
ask the orchestrator. The decisions v4 reverses are recorded in
[ADR-0002](docs/adr/0002-cppgo-built-shim.md), which supersedes
[ADR-0001](docs/adr/0001-header-first-bindings.md).

v4 changes one thing about v3 and nothing else: **who builds the C++ shim**. v3 generated
`zz_cppgo_shim.cpp` for the *user's* build to compile into their library, which made a cppgo-generated
file a dependency of a build cppgo does not own. v4 has **cppgo generate and compile the shim itself**,
into an object of its own that links the user's library. The user's sources and build are never
modified and never include generated code.

## What this is

`cppgo` calls C++ from Go with **no cgo at build time** and **no `import "C"`**. The primary path is:
point cppgo at the headers and the compilation configuration, and it generates the equivalent Go API.
The user writes no binding declarations. A hand-written declaration file remains as a fallback for a
project that does not want libclang in the loop; it is a separate, documented mode and it is not the
product.

Three claims, each measurable in this repository:

- **complete**: every construct in the bound headers is reachable, including the ones the System V
  register convention cannot express;
- **Go-side generated**: the artefacts cppgo emits are Go, Go assembly, and one C++ translation unit
  that cppgo compiles itself. The user's build sees none of them;
- **measured**: every mechanism in the table below has a test in `internal/callback` or
  `internal/shim`, and a hard limit is stated with the measurement that found it.

## The three artefacts

| artefact | who owns it | what it is |
|---|---|---|
| **the user's library** | the user | their sources, their compiler flags, their build. `libuser.so` in the tests. cppgo never writes into it and never asks it to include anything |
| **the cppgo shim** | cppgo | `zz_cppgo_shim.cpp`, generated from the same header parse as the Go API, **compiled by cppgo** into `libcppgo_shim.so` (or `.a` for a static link), linked against the user's library. One `extern "C"` thunk per binding the direct ABI cannot express |
| **the generated Go package** | cppgo | the API the caller sees, plus one `NOSPLIT` trampoline per thunk and per direct symbol; loads the shim, which pulls in the user's library through its `DT_NEEDED` |

The dotted line is the contract's edge: **the user's build is untouched, and generated code is not in
it.** Everything else follows from that line.

**Status of the tree.** This contract states the target. The mechanisms it relies on are measured and
have tests in this repository (`internal/callback` for the reverse direction, `internal/shim` for the
construct matrix, the emitters' existing direct-carrier selection with its own tests). The emitters in
`internal/gen` and `cmd/cppgo` still produce v3's shim for the user's build; moving them to emit and
compile the cppgo-owned shim is the follow-up workstream, and this file is what it implements.

Consequences worth stating:

- the user's `CMakeLists.txt`/`Makefile` gains nothing. A project that only wants to *consume* a
  cppgo-generated package installs two shared objects instead of one;
- the shim and the user's library must agree on the C++ ABI and the standard library, because the shim
  calls into the library across that boundary. The generator compiles the shim with the flags it
  discovered for the header (`compile_commands.json`, `.clangd`) and the same standard library, and
  refuses to mix a libstdc++ shim with a libc++ library (see *The `std::` ABI*);
- a header-only library with no `.so` at all is a supported case: the shim is all there is, and the
  "library" the generated package loads is the shim.

## The two layers

The runtime and the generator stay separate products with separate dependencies, and the line between
them is part of this contract.

| layer | package | owns | may depend on |
|---|---|---|---|
| **runtime** | `github.com/Manu343726/cppgo` (repository root) | dynamic loading, symbol resolution, the C ABI and its trampolines, the reentrant carrier, stack probing, foreign-object storage, wire helpers, error types, the callback table | the standard library, `internal/asm`, `internal/rtl`, `internal/callback`, `purego` |
| **generator** | `internal/gen`, `internal/cxx`, `cmd/cppgo` | parsing C++ through libclang, flag discovery, binding selection, carrier routing, emitting Go, assembly and the shim, **and the shim's own C++ compile** | anything, including libclang through `purego` and the C++ compiler |

The runtime **never imports `internal/cxx`, directly or transitively, and has no libclang
dependency**. libclang is generation-time only. The runtime builds and its tests run with
`CGO_ENABLED=0` on a machine with no libclang installed; the generator needs libclang and says so with
`ErrLibclangNotFound` when it is absent, and needs a C++ compiler to build the shim and says so with a
message naming the compiler when it cannot find one.

What generated code uses from the runtime, and nothing else:

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

and from the two internal packages, through the generated assembly only: `asm.Call` (the direct
carrier), `asm.CallReentrant` (the callback-capable carrier), `asm.Probe`, and `callback.Register` /
`callback.Release` / `callback.TakePanic`.

`Object` pins its storage with `runtime.Pinner`, so there is no package-level object registry. The one
piece of process-global state in the runtime is the callback table, and *Why the callback table is
global* states why it cannot be anything else.

## Carriers

Every binding is assigned exactly one **carrier** before anything is emitted. The generator never
leaves a declaration unbound and never emits a call it has not established a carrier for.

### Direct

The generated trampoline calls the mangled C++ symbol in the user's library. Measured at 15.9 ns/op
against 151 ns/op for the reentrant carrier on the same trivial callee (`internal/callback`'s
benchmarks). All of these must hold:

- the declaration has an out-of-line body and the mangled name is in the user's `.dynsym` (the ELF
  tier checks it, as in v3 — a header-only declaration whose mangled name happens to be exported is
  direct, and that rule is kept, with its tests);
- it is **not virtual** (the call is to a fixed address; dispatch is a shim concern);
- it is **`noexcept`** (the direct path has no handler, and an escaping exception terminates the
  process — measured);
- the signature is register-representable: scalars, enums, pointers and references, and a POD of at
  most 16 bytes with no MEMORY-class part. No `std::` type.

### Shim

Everything else is carried by a thunk in `zz_cppgo_shim.cpp`, which is C++ compiled by cppgo against
the same headers and the same flags, and linked against the user's library:

| what the declaration is | what the thunk does | measured by |
|---|---|---|
| inline / header-only body | includes the header and calls it: the body is compiled into the shim | `TestInlineHeaderOnlyIsReachableOnlyThroughTheShim` |
| virtual method, virtual destructor | calls it as C++ does — real dispatch through the vtable | `TestVirtualDispatchAndVirtualDestructor` |
| template | instantiates the concrete specialisation and calls it | `TestTemplateInstantiation` |
| `std::string` / `string_view` / `vector` / `span` / `array` / `optional` / smart pointers | constructs the C++ value, converts, copies in or out | `TestStdStringAndVector` |
| POD above 16 bytes / MEMORY class by value | copies it into the call; a result is written through an out-pointer | — |
| non-POD by value | copies the Go-held object in, or constructs the result in Go storage | `TestNonPODByValue` |
| exception-throwing call | wraps the call in `try`/`catch`, converts `std::exception` and `...` into the thread-local message | `TestExceptionsBecomeErrors` |
| multiple inheritance / virtual base | performs the `static_cast`/`dynamic_cast` that adjusts `this` | `TestMultipleInheritanceAdjustment` |
| more than 6 INTEGER or 8 SSE arguments | takes a pointer to an argument block and unpacks it | — |
| anything with an out parameter | writes through the pointer the Go side provides | every row above |
| a callback parameter | constructs the `std::function` from the Go function pointer | `TestCallbackThroughTheShim` |

Both carriers are ordinary C ABI functions from Go's point of view: `Open` resolves a `uintptr`,
`ProbeStack` runs, and one NOSPLIT trampoline calls it. The carrier is invisible in the API and
visible in the report:

```
user::Circle::area      shim    shim_shape_area        virtual
user::checked_div       direct  _ZN4user11checked_divEii   noexcept, out of line, register class
user::maxof<double>     shim    shim_maxof_double      template instantiation
```

### The callback-capable call

A binding whose signature takes a function pointer or a `std::function` is called through
`asm.CallReentrant` instead of `asm.Call`. That is one runtime entry, not a second ABI:

- `asm.Call` calls the C function from a NOSPLIT trampoline on the goroutine's stack. Fast, and the
  only carrier a plain function call needs;
- `asm.CallReentrant` calls `runtime.cgocall` (reached by linkname, so still no cgo at build time)
  with a generated assembly wrapper that loads the six INTEGER and eight SSE registers. The M is
  inside a foreign call, which is exactly the state `runtime.cgocallback` requires.

A callback invoked through the direct carrier kills the process: the runtime's callback entry reaches
`exitsyscall` on an M that never entered a syscall, and the runtime dies with
`fatal error: morestack on g0`. That is measured in a child process by
`TestCallbackOnTheDirectTrampolineIsFatal`; it is why the carrier choice is part of the contract and
not an optimisation.

The reentrant carrier runs the C frames on the M's system stack instead of the goroutine's, so
`ProbeStack`'s 32 KiB reserve does not apply to them — measured to 4 MiB in
`TestReentrantCarrierStackBudget` (the budget is the thread stack, 8 MiB by default). It costs one
16-byte allocation per call, because the argument block that carries the target cannot live on the
goroutine's stack: a callback may grow that stack, and the C side holds the block's address.

## What the user writes

**Primary path — nothing.** The input is the headers plus the compilation configuration:

```
cppgo gen -header include/user.hpp -package user -out ./user
          [-library libuser.so] [-compile-commands build/compile_commands.json]
          [-instantiate '<template specialization>']... [-rename '<signature>=<GoName>']...
```

`cppgo gen` parses the headers through libclang, selects every public declaration (a header is the
unit of selection, not a hand-picked subset), emits the Go package and the shim, compiles the shim,
and writes the invocation into the generated file so the regeneration command lives in the repository.
`cppgo check` runs the same work, writes nothing, and exits non-zero when a file is stale.

**Fallback — a declaration file.** For a project that does not want libclang, the same emitters take a
hand-written description of the symbols and signatures to bind. It produces the same artefacts and is
documented separately; it is never the recommended path, and the header is still what the shim
includes.

## The generated API

The input is C++. For this header:

```cpp
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

`cppgo gen` writes this package — this is the API, not an illustration:

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

Rules that the API shape follows:

- **Mirrorable record → Go struct with matching exported fields.** A record is mirrorable when its
  fields are all public, its layout is the natural Go layout, and no field is a bitfield, a reference,
  a pointer, a union member, or a non-mirrorable type. The generator reads `sizeof`, `alignof` and
  every offset from libclang and asserts them at compile time in the generated file.
- **Every other record → opaque value type**: a Go struct with unexported storage, `Close()` from the
  destructor, `Clone()` from the copy constructor (when one exists), and generated accessors for every
  public field (`Field()` / `SetField(v)`). Bitfields, packed layouts, private members, non-POD
  members, and a layout libclang could not complete all take this path; nothing is left unreachable.
- **Class → Go struct with methods**, `New<Type>(...) (*Type, error)` for a constructor, `Close() error`
  for the destructor, package functions for free and static functions, named Go integer types for
  enums.
- **`cppgo.Handle` appears in no exported signature.**
- **Ownership and views.** An owning value carries the destructor of the type it was constructed as and
  a Go-side reference count. `As<Base>()` returns a view of the same object and adds a reference;
  `Close` drops one, and the destructor runs at the last one.
- **Zero values are invalid**: a method on a nil pointer or a closed value returns `cppgo.ErrClosed`.
  No finalizer is generated.

## The per-construct mechanism table

Every row names the mechanism, the evidence, and the limit when there is one. "Direct" means the
generated trampoline calls the user's symbol; "shim" means the cppgo-built thunk calls it as C++.

| # | construct | mechanism | evidence | limit, if any |
|---|---|---|---|---|
| 1 | virtual dispatch | virtual calls go to the shim (`object->method()`), where the vtable is. Direct vtable dispatch from Go also works: the vptr is the object's first word and points at the address point, so slot *k* is `*(vptr + 8k)` with the destructor pair first | `TestVirtualDispatchAndVirtualDestructor`, `TestVirtualDispatchFromTheVTable`, plus a C++ dump of the same vtable | the *slot index* is generation-time knowledge (declaration order); a slot call from Go is never needed when the shim exists, and is kept as the direct path for an exported non-virtual-looking wrapper |
| 1 | virtual destructor | `delete` through the base pointer in the shim; the derived destructor ran (counter read back) | `TestVirtualDispatchAndVirtualDestructor/virtual_destructor_reaches_the_derived_one` | — |
| 1 | `this` adjustment, multiple inheritance | the shim's `static_cast`; `Disk`'s `Circle` subobject is at a non-zero offset and both addresses are returned to Go | `TestMultipleInheritanceAdjustment` | virtual bases: the offset is a vtable lookup, done in C++ by the same cast; it is not computed in Go |
| 2 | inline / header-only | shim includes the header; the body is compiled into the shim | `TestInlineHeaderOnlyIsReachableOnlyThroughTheShim` (the user's library exports no symbol, the shim does) | a declaration whose definition is not visible to the shim cannot be bound: the user must include the header that defines it, or provide an out-of-line definition |
| 3 | `std::string` | copied in and out through a buffer; the `std::string` lives and dies inside the shim | `TestStdStringAndVector` | the shim and the library must use the same standard library and the same `_GLIBCXX_USE_CXX11_ABI` |
| 3 | `std::vector<T>` | copied in and out through the caller's slice | `TestStdStringAndVector/std::vector_in_and_out` | same as `std::string` |
| 3 | `std::string_view`, `std::span` | borrowed: `(pointer, length)`, no copy, valid for the call | designed from the v3 table; the shim receives the raw pair and constructs the view | the callee must not retain the view |
| 3 | `std::array<T,N>` | by value through the shim | designed | same ABI rule as any by-value aggregate |
| 3 | `std::optional<T>` | `*T`: nil is `nullopt`; the shim constructs and reads it | designed | — |
| 3 | `std::unique_ptr<T>` | a parameter consumes the Go value, a result is owning (`Close` destroys it) | designed | the deleter must be the default one |
| 3 | `std::shared_ptr<T>` | a parameter shares (the reference count is copied), a result is owning | designed | a `shared_ptr` the *Go side* must construct out of nothing needs a control block only C++ can allocate: that is a shim entry point, not a layout trick |
| 4 | non-POD by value | constructed and copied inside the shim; a by-value result is constructed into Go storage through the sret pointer | `TestNonPODByValue` (the copy constructor's +1000 is observable) | the type's copy constructor and destructor must be visible to the shim, which they are when the header declares them |
| 5 | exceptions | `try`/`catch` in the shim; the message travels in a thread-local string and arrives as a Go `error` | `TestExceptionsBecomeErrors` | **no direct binding may call a function that can throw**: measured, a throw with no C++ frame terminates the process (`TestExceptionFromCIsFatal`, `TestExceptionAfterACallbackIsFatal`, `TestExceptionThroughTheGoCallbackIsFatal`). A library that reports failures as `noexcept` + status needs no shim |
| 6 | templates | the shim instantiates the specialisation the Go side asked for and calls it | `TestTemplateInstantiation`, `TestTwoPhaseGenerics` | the instantiation must be named: the Go generic API's instantiations are collected and the shim is emitted from that list; a Go instantiation with no C++ counterpart fails at load with the exact `template ...;` line to add |
| 7 | inheritance / `As<Base>` | the shim's cast; `As<Derived>` is a `dynamic_cast` and reports `(*Derived, bool)` | `TestMultipleInheritanceAdjustment` | a `dynamic_cast` requires RTTI; a library built with `-fno-rtti` reaches the shim only through `static_cast` |

## Callbacks

A Go function handed to C++ — as a function pointer or wrapped into a `std::function` by the shim — is
a **slot in a table of five-byte `CALL` instructions** (`internal/callback`). The instruction a C
caller lands on names the slot; the trampoline saves the fourteen argument registers and calls
`runtime.cgocallback` through a copy of the runtime's host-ABI entry; the runtime runs the Go function
on the goroutine that made the call, locked to its M, with the C frames left where they are.

The ABI, exactly:

- arguments in `RDI, RSI, RDX, RCX, R8, R9` and `XMM0..XMM7`, the register file only. A callback that
  takes a stack-passed argument is a generation error naming the sixth argument and the fix (take a
  pointer to a struct);
- the result is `RAX` or `XMM0`, one register: `Args.Ret` and `Args.RetF`;
- the Go side marshals the frame itself: `func(*callback.Args)` reads `I` and `F`, writes `Ret` or
  `RetF`. There is no reflection on the callback path.

Lifetime rules:

- **Registration** returns an address in a 64-slot process-wide table. Registering a function twice
  gives two independent slots, because C++ may release one while the other is still held;
- **Release** waits for the calls already inside the Go function to return, so a caller that is done
  with a callback can release it and then free what it captured. It cannot wait for C++: a C++ object
  that still holds the address and calls it after `Release` reaches the dispatcher's released-slot
  path, which reports through `TakePanic` and returns a zero result — a programmer error on the C++
  side, never a use-after-free;
- the callback's Go frame is as valid as the foreign call: it may allocate, grow its stack and take a
  traceback (measured: the goroutine id and thread id are the caller's, and `runtime.Stack` works
  inside it).

**Panic.** A panic inside a callback never unwinds into C++: the dispatcher recovers it, zeroes the
result, records a `*callback.Panic` (value plus stack), and the binding that passed the callback
checks `callback.TakePanic()` when the foreign call returns and re-panics there. Measured by
`TestCallbackPanicIsCaptured` and `TestCallbackPanicReachesTheCaller`.

**Reentrancy.** Go → C++ → Go → C++ nests: the runtime counts foreign calls per M, and a callback may
make another reentrant call. `TestCallbackReentrancy` measures two levels.

**Exceptions.** A C++ exception thrown *by* a callback is not a thing the ABI can express; a callback
that wants to report a failure writes a status into `Args.Ret` and the C++ side checks it. An
exception raised across a Go callback frame terminates the process (measured); the rule is the same as
everywhere else: the C++ side catches, the Go side reads a status.

## The `std::` ABI

`std::` types cross at the shim, and the shim is C++ compiled by cppgo. The dependency is therefore
explicit and checkable rather than a layout trick:

- the shim is compiled with the flags the generator discovered for the header, and with the same
  compiler family the library was built with. The generator records the compiler it used and the
  `_GLIBCXX_USE_CXX11_ABI` value it was given, and rejects a shim whose standard library does not
  match the one the library links (`libstdc++` vs `libc++`, detected from the compiler and its
  default standard library);
- `std::string`'s layout never appears in Go. It appears in the shim, which is where a
  `std::string` can legally be constructed;
- a `std::` type outside the table still crosses as an opaque value, constructed and destroyed by the
  shim;
- one case is worth naming because it is the only place a layout *is* used: a `std::function` the Go
  side must build without a C++ factory. It is 32 bytes on libstdc++ (`_Any_data` 16, `_M_manager` 8,
  `_M_invoke` 8), and the two code pointers can only come from instantiations the library or the shim
  exports. v4 does not need it — the shim constructs the `std::function` from a function pointer, as
  `TestCallbackThroughTheShim` measures — and it stays recorded in `internal/callback`'s
  `TestStdFunctionFromGo` as the fallback for a library that already exports those instantiations.

## Exceptions

**Every call that can throw goes through the shim, and the shim catches.** The rule is exact:

- a **direct** binding is `noexcept` by construction; a non-`noexcept` function is never direct;
- a **shim** thunk is `noexcept` and returns a status: `0` for success, non-zero for a caught
  exception, with the message in a thread-local string exported as
  `extern "C" const char *cppgo_last_error()`. The generated Go wrapper reads it and returns
  `*cppgo.CallError{Symbol, Err}`;
- a **destructor** is `noexcept` unless declared otherwise; `Close() error` always exists and reports
  `cppgo.ErrClosed` for a dead receiver and any exception a `noexcept(false)` destructor raises.

Measured: `TestExceptionsBecomeErrors` (caught and reported) and the three child-process tests in
`internal/callback` (a throw with no C++ frame terminates the process, including through a callback).
**There is no exception support without a C++ frame**, and v4 says so plainly rather than pretending
otherwise: a library whose API throws and whose headers cannot be included by the shim cannot be bound
safely, and the fix is a `noexcept` entry point in the library.

## Templates and the two-phase generics flow

A C++ template only exists where it is instantiated. For a template-heavy library (Eigen above all)
the Go API is generic and the C++ instantiation is a build step, so the flow has two phases:

1. **the Go program instantiates the generic API** — `Matrix[float64, 4, 4]`,
   `Vector[float32, 3]`. The user writes only the Go program;
2. **cppgo collects the instantiations** — by parsing the generated package with `go/packages` and
   reading the type arguments at the union point, or from an explicit list the package declares (the
   `-instantiate` manifest entries, for a program that wants to pin the set). The output is a list of
   C++ specialisations: `Eigen::Matrix<double,4,4>`, `Eigen::Vector3f`;
3. **cppgo emits the shim from that list** — `template struct Eigen::Matrix<double,4,4>;` plus a thunk
   per operation the Go type uses — and **compiles it** into the cppgo artefact that links the
   library;
4. the generated Go package calls the thunks. The failure mode when a Go instantiation has no C++
   counterpart is a load-time error naming the exact line to add, measured by
   `TestTwoPhaseGenerics` (its `int64` subtest).

What each target exercises, and what is out of scope with the reason:

| target | what it exercises | out of scope, and why |
|---|---|---|
| **Eigen** | the two-phase flow above; a header-only library with no `.so`, so the shim is the whole binary; expression templates, which are compiled in the shim and never appear in a Go signature; POD by value (`Matrix4d` is 128 bytes, MEMORY class, copied by the shim) | Eigen's expression templates across the Go boundary: a Go signature names `Matrix[T,R,C]`, never `CwiseBinaryOp<...>`; the shim materialises the expression with `.eval()` |
| **OpenCV** | whole-header binding at scale: hundreds of overloads, default arguments, `cv::Mat` as an opaque value with `Close`, `std::string` and `std::vector<T>` everywhere, enums with explicit values | GPU modules that need a CUDA runtime at link time: the shim links what the library links, and a missing device is the library's error, not a binding failure |
| **ArrayFire** | C++ API over a C API; handles are opaque values; `af::array` by reference; exceptions are `af::exception`, caught by the shim and reported as Go errors | none measured yet; the exception row is the one to verify first, because ArrayFire throws by design |

## Whole-header API at scale

A header is the unit of selection, so the generator has to answer the questions a large header asks:

- **Overloads** take a suffix built from the Go parameter types, in order (`Dot`, `DotFloat64Slice`,
  `DotVector`); one declaration keeps the bare name. A collision that no suffix separates is a
  generation error naming both declarations and asking for `-rename`.
- **Default arguments** produce one Go function per prefix of the parameter list that the defaults
  allow, generated only for the prefixes actually used (`f(a)`, `f(a,b)`), never a variadic soup; a
  prefix that would collide is dropped and the report says so.
- **Namespaces** contribute nothing to the Go name; two declarations from different namespaces that
  map to one identifier need `-rename`, and without one the error names both.
- **Enums** become named Go integer types with typed constants, scoped and unscoped alike, with the
  underlying type from libclang.
- **Macros** are not parsed — libclang sees a macro's expansion, not its declaration — so a macro that
  *is* the API (glibc's `__attribute__` wrappers, OpenCV's `CV_EXPORTS_W`) is invisible and the
  underlying declaration is bound instead. A macro-only API (a function-like macro that expands to a
  call) is not bindable, and the generator reports it as such with the declaration it could not see;
  the fix is a function, which every large library provides.
- **Classes with inline bodies, templates, and `std::` members** need nothing from the user: they are
  shim work.
- **What is emitted** is every public declaration in the headers, plus the type closure of every bound
  signature. `-bind` and `-exclude` narrow it; an unmatched entry is an error, so a typo cannot
  silently shrink the API.

## Type map

Sizes are validated against libclang's `sizeof` for the translation unit; a platform where `long` is
not 64-bit is a generation error, not a silent reinterpretation.

| C++ | Go | notes |
|---|---|---|
| `bool` | `bool` | |
| `char`, `signed char` | `int8` | plain `char` is signed on the supported targets |
| `unsigned char` | `uint8` | |
| `short`, `unsigned short` | `int16`, `uint16` | |
| `int`, `unsigned int` | `int32`, `uint32` | |
| `long`, `long long`, `unsigned long`, `unsigned long long` | `int64`, `uint64` | LP64, validated |
| `int8_t`…`uint64_t` | the same width | fixed-width aliases resolve by size |
| `size_t`, `uintptr_t`, `intptr_t`, `ptrdiff_t` | `uint64`, `int64` | validated |
| `float`, `double` | `float32`, `float64` | |
| `wchar_t`, `char16_t`, `char32_t` | `int32`, `uint16`, `uint32` | validated by size |
| enum (scoped or unscoped) | named integer type + constants | underlying type from libclang |
| `T *`, `T &` | `*T` | borrowed; `[]T` for the pair form |
| `(T *p, size_t n)` | `[]T` | borrowed; `const` decides whether the callee may write |
| `(char *p, size_t n)` | `string` | borrowed for the call; not NUL-terminated |
| mirrorable record, by value | `T` | the Go struct *is* the C layout |
| non-POD record, by value | `*T` | the shim copies into the call; `T` owns Go-pinned storage |
| `T[N]` (record field) | `[N]T` | fixed-size array |
| `std::string` | `string` | copied in and out |
| `std::string_view` | `string` | borrowed for the call |
| `std::vector<T>` | `[]T` | copied in and out |
| `std::span<T>` | `[]T` | borrowed |
| `std::array<T,N>` | `[N]T` | by value |
| `std::optional<T>` | `*T` | nil is `nullopt` |
| `std::unique_ptr<T>` | `*T` | a parameter consumes, a result owns |
| `std::shared_ptr<T>` | `*T` | a parameter shares, a result owns |
| function pointer | `func(...)` | the callback table's address |
| `std::function<R(Args...)>` | `func(...)` | the shim constructs the object |

`long double` and `__int128` have no Go value type; a signature that uses one is a generation error
naming the parameter and the fix (widen/narrow at the C++ boundary to `double`/`int64`, or wrap it).

A pointer parameter followed by a length parameter of integer type becomes a slice: the pointer is the
data, the length is `len(slice)`, and the slice is borrowed for the call. One length may serve several
pointers; the generated binding checks before the call that every slice sharing a length has the same
length.

## Naming and overloads

- A name is CamelCase, split on `_`, leading and trailing underscores stripped.
- `New<Type>` for a constructor, `Close` for the destructor, `<Type><Name>` for a static member.
- Constructor overloads follow the same suffix rule as functions, applied to `New<Type>`; a copy
  constructor is `Clone()`, and a move constructor is not exposed separately.
- A **class template specialisation** is `<TemplateName><Arguments>`: `Box<int>` → `BoxInt`,
  `Matrix<double,4,4>` → `MatrixFloat64FourFour` in the concrete API and `Matrix[float64,4,4]` in the
  generic one.
- A **conversion operator** is named after its target Go type (`Int()`, `Float64()`, `String()`,
  `Bool()`, `AsVector`).
- **Operators take a fixed name** (`Add`, `Equal`, `At`, `WriteTo`, …); anything not in the table, or a
  collision, is a generation error asking for `-rename '<qualified signature>=<GoName>'`. `operator
  new`/`operator delete` are deliberately absent: the Go side allocates.
- A **static data member** is a package function `<Type><Field>()`, plus `<Type>Set<Field>(v)` when it
  is not `const`.
- `Open` is reserved as the package entry point; a `-rename` that matches nothing is an error.

## Errors

Typed errors: `*OpenError`, `*SymbolError`, `*NativeError` (the message itself), `*CallError` (which
wraps the native error), `*callback.Panic`, and the sentinel `ErrClosed`. The `-status` convention
remains available for C++ APIs that report failures as a 32-bit return code: the Go binding returns
`error` instead of the integer and wraps the message in `*CallError`.

## The ABI the direct path calls

Itanium C++ ABI on SysV AMD64, which the generator validates against and the trampolines implement:

- INTEGER and pointer arguments: `RDI, RSI, RDX, RCX, R8, R9`; SSE: `XMM0…XMM7`, independent banks;
- a hidden sret pointer takes the first INTEGER slot; `this` takes the next; the rest follow;
- results: `RAX` for integer and pointer, `XMM0` for float, up to two registers for a 16-byte POD;
- a slice or string parameter is `(pointer, length)`, pointer first, adjacent in the integer file;
- argument registers are not spilled: more than 6 INTEGER or 8 SSE slots, or a by-value aggregate
  outside the register class, is a shim binding. **Results** are different: an sret result is written
  through the hidden pointer into Go storage.

The reentrant carrier has one extra rule, measured the hard way: `runtime.cgocall` reaches the
register-loading wrapper by a tail jump, so the stack is 16-aligned at its entry (not 8 mod 16 as
after an ordinary `CALL`), and the target must be called without an extra adjustment. A callee
compiled with aligned SSE spills faults immediately if this is wrong;
`TestCallReentrantGivesTheCalleeAnAlignedStack` is the regression test.

## The trampoline

Measured 2026-11-03, Go 1.27, an 8-core x86-64 CPU pinned to 964 MHz with boost disabled — the
absolute nanoseconds are inflated by roughly 4.5×, so cycles and ratios are the honest numbers:

| path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| **cppgo trampoline, direct** | **13.9** | **13** |
| cppgo trampoline, stack switch | 14.9 | 14 |

Measured again 2026-11-08 on the same machine with the carriers side by side
(`go test -bench . -benchtime 200000x`): direct 15.9 ns/op, reentrant 151 ns/op (one 16-byte
allocation per call), callback round trip 481 ns/op. A shim thunk adds one C++ call frame inside the
library; the Go-side cost is the same trampoline.

One signature, as generated:

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

Rules that are not negotiable:

- arguments are read **before** SP is switched and results are stored **after** it is restored;
- `NOSPLIT`, zero frame, no allocation from the trampoline itself, no Go memory touched;
- `ProbeStack()` runs before the trampoline, **from the same goroutine and before any pointer into the
  caller's frame is taken**: the probe grows the stack, which moves that frame. This is measured, not
  theoretical — it is what broke `internal/shim`'s first draft, where the probe ran after the out
  pointers were formed, and it is why a `t.Run` subtest (a new goroutine) probes again;
- correctness evidence, required on every run: a NOSPLIT asm function is never asynchronously
  preempted, GC torture with `GOGC=1` under 32 goroutines, SIGPROF/SIGURG torture, `-race`, and
  `GOMAXPROCS=1`.

## Loading

`github.com/ebitengine/purego` is the only third-party runtime dependency, and this is deliberate: it
solves `dlopen`/`dlsym` without cgo by importing `internal/fakecgo`, which is larger and less
forgiving than any loader we would write.

We use only `purego.Dlopen`/`Dlsym`, never purego's call path — with one dependency to state plainly:
the reentrant carrier calls `runtime.cgocall` by linkname, and that function throws
`cgocall unavailable` unless the cgo runtime has been initialised. `runtime/cgo` does it under
`CGO_ENABLED=1`, and purego's fakecgo does it under `CGO_ENABLED=0`; a program that uses a
callback-capable binding without either would fault at the first call. Importing the cppgo runtime
package pulls purego in, so every generated package satisfies this by construction, and the
runtime's own test asserts it.

## Generation: input, selection, evidence, output

- **Input.** `-header` (repeatable), `-package`, `-library`, `-compile-commands`, `-bind`,
  `-exclude`, `-instantiate`, `-rename`, `-status`.
- **Compilation flags** come from `compile_commands.json` and `.clangd`, in clangd's order, never
  invented. A relative `-I` in a database entry is rebased against the entry's `directory` (clangd
  does this; libclang does not, and a database that is not rebased produced a phantom binding in v3 —
  v4 rebases it and has a test).
- **Evidence.** libclang answers types, sizes, offsets, visibility, inline-vs-out-of-line, virtual,
  exception specification, defaulted/deleted special members, template arguments and base lists. The
  ELF tier is a cross-check for direct bindings against the library's `.dynsym`.
- **Output.** `zz_cppgo.gen.go` (the API), `zz_cppgo_amd64.s` (the trampolines), `zz_cppgo_shim.cpp`
  (the thunks), and the compiled shim object. The generated Go file carries the exact invocation that
  produced it.
- **Shim build step.** cppgo compiles the shim itself: the same compiler family as the library, the
  discovered flags, `-shared` (or an object for a static link), linked against the library. The
  compiler, its version, its standard library and `_GLIBCXX_USE_CXX11_ABI` are recorded in the
  generated file. A compile error is a generation error with the compiler's message, never a symbol
  that fails at the first call.
- **Determinism.** The same inputs produce byte-identical Go and C++; bindings are sorted by qualified
  name and thunk names come from a stable hash of the qualified signature. `cppgo check` is the CI
  gate, and the golden files under `internal/gen/testdata` are its unit-level equivalent.
- **Comments.** The C++ doc comment becomes the Go doc comment, and every binding carries exactly one
  evidence line — `// C++: user::Circle::area() const, _ZNK4user6Circle4areaEv` — and, when the ELF
  cross-check ran, it ends `, verified against libuser.so`.

## Hard limits, measured

These are the places where the contract says "no", with the measurement behind it:

1. **An exception with no C++ frame terminates the process.** Three child-process tests in
   `internal/callback` measure it: from a direct call, after a callback returned, and through a Go
   callback frame. The only support is the shim's `try`/`catch`.
2. **A callback requires the reentrant carrier.** A callback reached through the direct trampoline
   dies with `fatal error: morestack on g0` (`TestCallbackOnTheDirectTrampolineIsFatal`).
3. **A callback carries at most six INTEGER and eight SSE arguments and one register result.** The
   trampoline reads the register file only.
4. **`runtime.cgocall` needs the cgo runtime initialised** (runtime/cgo or purego's fakecgo), stated
   above.
5. **A `std::` layout is only ever in C++.** The one layout the Go side can be forced to use — a
   `std::function` built without a C++ factory — needs the library to export two template
   instantiations; v4 avoids it by having the shim construct the object.
6. **A macro that is the API is not bindable**, because libclang sees expansions.
7. **A template instantiation that no one asked for does not exist.** The two-phase flow makes the
   failure a load-time error naming the line to add.
8. **`long double` and `__int128` have no Go value type.**
9. **The shim and the library must share a C++ ABI and a standard library.** Mixing libstdc++ and
   libc++ is a generation error, not a runtime surprise.

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

- the **user's build compiling generated code**. v3 did this and ADR-0002 reverses it;
- the Go directive DSL as a *primary* mode: `//cppgo:library`, `//cppgo:header`, `//cppgo:class`,
  `//cppgo:method` and friends survive only as the fallback declaration file's syntax;
- a hand-written `extern "C"` shim layer. The shim is generated infrastructure, compiled by cppgo;
- `cppgo.Handle` in a generated API.

Non-goals: arm64 and Windows code generation (the Go package still builds; the generator does not emit
trampolines for an unsupported target and the error names it).

## Tests and coverage

- `make test` runs two passes, both required: `CGO_ENABLED=0 go test -count=1 ./...` (the no-cgo
  contract) and `CGO_ENABLED=1 go test -race -count=1 ./...` (the race detector; the toolchain rejects
  `-race` with cgo off). Coverage is collected on the cgo-enabled pass with `-covermode=atomic`.
- Table-driven tests with `t.Run` per case, `testify` `require`/`assert`, `t.Parallel()` where cases
  are independent. Concurrency tests run under `-race` and `GOMAXPROCS=1` with at least 32 goroutines.
- Coverage gate: **≥ 99%** on `./...`, measured by `./scripts/coverage-gate.sh`. The gate's
  exclusions are the three it prints, each with its reason: `zz_cppgo*.go` (generator output),
  `internal/gen/testdata/` (generated fixtures), and `internal/testlib/` (test scaffolding). No
  hand-written production package is excluded, and `cmd/` is measured like everything else.
- **Carrier coverage is a test**: every binding in the fixture set is asserted to have a carrier, and
  the golden output is compiled and run for both carriers.
- **The shim is compiled by the test suite**, which is how "cppgo compiles the shim" is measured
  against a real compiler rather than against a model: `internal/shim` builds the user's library and
  the shim as two objects and asserts the artifact model (no cppgo symbol in the user's library, the
  thunks in the shim).
- **The callback mechanism is measured in both CGO modes**, including the child-process tests whose
  expected outcome is a fatal error.
- The runtime has a test that fails if `internal/cxx` appears in the root package's dependency graph,
  so the two layers cannot quietly merge.
