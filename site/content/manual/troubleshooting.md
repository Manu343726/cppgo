+++
title = "Troubleshooting"
description = "The failures you will actually hit: an inline member with no symbol, a virtual function, a std:: type by value, arity and width mismatches, register exhaustion, a missing libclang, and wrong compilation flags."
weight = 100
+++

## "It has no symbol": defined inline

**Symptom**

```
mathx.go:24: mathx::Vector::x: it is defined inside its class definition or has
internal linkage, so it is implicitly inline and the library exports no symbol
for it: move the definition out into a source file and rebuild, or bind it
through the shim mode
```

**Why.** A member function defined inside its class definition is implicitly
inline. The compiler may emit no out-of-line body at all, so the library has no
dynamic symbol for `dlsym` to find. This is not an edge case: idiomatic C++
defines short accessors and often the constructor that way.

**Fix, in order of preference:**

1. **Move the definition out of line.** In the header keep
   `double x() const;`, and in the `.cpp` write `double Vector::x() const
   { return x_; }`. Then regenerate. This costs one line and keeps the fast
   path.
2. **Add an out-of-line factory.** If you cannot edit the header (a third-party
   SDK), add a wrapper class of your own whose constructor is out of line:
   `Vector wrap_vector(double x, double y);` defined in your `.cpp`.
3. **Shim mode.** An `extern "C"` entry point can call the inline function
   inside its own translation unit, where the definition is visible:
   `extern "C" double mathx_vector_x(void *v) { return static_cast<Vector*>(v)->x(); }`

The same message appears for a namespace-scope `static` function, which has
internal linkage and cannot be exported either.

## "It is virtual"

**Symptom**

```
mathx::Shape::area: it is virtual: the call has to go through the vtable to
reach an override, so a fixed symbol cannot be called directly; use the shim
mode, or bind a non-virtual member
```

**Why.** A fixed mangled symbol is one implementation. A virtual call must read
the object's vtable and dispatch to the override, and the generated trampoline
does not do that.

**Fix.** Use the shim mode (`extern "C"` entry point that calls the virtual
function normally, with the compiler emitting the dispatch), or bind a
non-virtual member. A virtual destructor has the same problem and the same
answer.

## A `std::` type by value

**Symptom**

```
mathx::Vector::describe: parameter 1 (name string): the C++ parameter is
"std::string", a standard-library type: its ABI (and its lifetime) is the shim
mode's business
```

**Why.** `std::string` is not one pointer in the ABI; it is a small object with
its own layout, copy semantics and allocator. The same is true of
`std::vector`, `std::span` and every other non-POD standard type.

**Fix.** Change the signature to take the parts that cross cleanly
(`const char *` plus `std::size_t`), take a pointer/reference and let a shim
unwrap it, or use shim mode. Taking `const std::string &` is a pointer slot, so
it can be bound with `**T`-style reasoning — but the C++ side still needs to
know the type, which the direct mode cannot express from a Go `[]byte`.

## Arity mismatch

**Symptom**

```
mathx::Vector::dot: the binding describes 2 arguments but the C++ signature
takes 3
```

**Why.** The count in the message is the number of C++ parameters after the Go
types are expanded: a Go `string` or slice spends **two** C++ parameters (pointer
and length). So `func Dot(ys []float64)` describes 2 — which matches
`dot(const double *, size_t)` — and the mismatch appears when the C++ signature
has one more parameter than your Go declaration accounts for.

**Fix.** Count on the C++ side, then expand the Go side: one slice = two. If the
C++ function takes `(const double *ys, size_t n, double scale)`, the Go
declaration needs `(ys []float64, scale float64)`. A `string` expands the same
way, and a Go declaration with three scalars and one slice describes five C++
parameters, not four.

## Width or register-class mismatch

**Symptom**

```
parameter 2 (n int): the Go type is int (8-byte) but the C++ type is "int"
(4-byte): the two must have the same width, because the value is passed in one
register
```

**Why.** Go `int` is 64-bit. C++ `int` is 32-bit on every platform cppgo
supports. The value travels in one register, so the widths have to agree exactly;
silently truncating would be a bug that only shows on large values.

**Fix.** Match the width: C++ `int` → Go `int32`, C++ `long` / `long long` /
`size_t` → Go `int64` / `uint64` / `int` / `uint`. A float bound to an integer
parameter gives the other message (*"travels in the other register class"*):
change the Go type, or change the C++ type, but do not `-force` it.

## Register exhaustion

**Symptom**

```
function Build needs 7 integer argument registers, but the SysV ABI only
provides 6 (RDI, RSI, RDX, RCX, R8, R9); group or reduce the arguments
```

**Why.** The trampoline has no path for stack arguments. Strings and slices
consume two integer slots each, so a function with four slices already needs
eight.

**Fix.** Group related values into a struct and pass a pointer to it, or take
one slice of a struct instead of several parallel slices, or move to shim mode
where the C ABI's own stack rules apply. Do not `-force`: the seventh argument
would land nowhere.

## libclang not found

**Symptom**

```
cppgo gen: cppgo: include/mathx/vector.hpp: cxx: libclang not found (searched
libclang.so, /usr/lib/libclang.so, ...; install libclang-dev or set
CPPGO_LIBCLANG)
```

**Why.** The direct ABI mode parses the header with libclang. Without it there
is nothing to validate, so the generator fails instead of guessing.

**Fix.** Install your distribution's libclang development package (the error
message suggests `libclang-dev`), or set
`CPPGO_LIBCLANG=/path/to/libclang.so`. If the build machine cannot have
libclang at all, switch the package to shim mode on purpose: `//cppgo:mode
shim`, no `//cppgo:header`, `//cppgo:external` bindings.

## Wrong compilation flags

**Symptom.** The assumed-flags warning, or a binding rejected for a type you
know is different:

```
cppgo: warning: the compilation flags for /src/mathx/vector.hpp are an
assumption (assumed: -std=c++17 -I/src/mathx); a validation this misses comes
from a parse without the header's real flags
```

**Why.** The header was parsed without the include paths, defines or standard
the build uses. libclang then sees a partial or wrong declaration — sometimes
none at all, which reads as "not declared in ...".

**Fix.** Generate a `compile_commands.json` (`cmake
-DCMAKE_EXPORT_COMPILE_COMMANDS=ON`, or `bear -- make`) and pass it:

```console
$ cppgo gen -dir ./bindings/mathx -compile-commands ./build
```

or put a `.clangd` next to the header:

```yaml
CompileFlags:
  CompilationDatabase: build
  Add: [-DMATHX_USE_DOUBLES]
```

Then check that the report says `abi from compile_commands.json` or
`abi from .clangd` instead of `fallback`.

## Other failures worth recognising

| symptom | cause | fix |
|---|---|---|
| *it is a static member function, which has no this* | bound with `//cppgo:method` | use `//cppgo:function mathx::Vector::dims` |
| *N overloads accept the Go argument types* | two C++ overloads fit the Go signature | rename the Go function to make the intent unique, or bind a differently-typed one |
| *//cppgo:ctor result 0 must be a //cppgo:class handle type* | the constructor returns a pointer or a struct | declare the result as the class type |
| *//cppgo:dtor returns nothing* | the destructor declares an `error` | drop it; destruction cannot fail in this mode |
| *the receiver of a bound method must be named* | `func (Vector) Norm()` | name it: `func (v Vector) Norm()` |
| symbol resolves to null at run time, only with clang++ | clang++ emits only `C2` for an abstract class while libclang names `C1`; g++ emits both | bind a concrete class, or use shim mode |
| *opening libmathx: ... cannot open shared object file* | the library name is not on the loader's path | set `CppgoLibraryPath`, `LD_LIBRARY_PATH`, or use an absolute path |
| the constructor's documented size is 0 or absurd | the probe translation unit did not see the real class (wrong defines, a different header) | fix the flags; the report's size comes from `sizeof` of what libclang saw |

## When in doubt, ask the report

The per-binding report is the tool explaining itself. Before changing code, run
`cppgo gen` without `-q` and read:

1. **which tier answered** (`compile_commands.json`, `.clangd`, `fallback`);
2. **the mangled name** — if it is empty, the entity has no ABI name (a
   template);
3. **the notes** — allocation size, and whether the trailing error is always
   nil.

Nine times out of ten the fix follows from the tier: a `fallback` parse is a
flags problem, not a code problem.

Back to the [manual]({{< ref "manual/_index.md" >}}), or read [when the
generator refuses]({{< ref "tutorials/when-generation-refuses.md" >}}) for the
decision procedure in narrative form.
