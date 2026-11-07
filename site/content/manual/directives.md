+++
title = "Directives"
description = "Every //cppgo: directive: library, header, class, ctor, dtor, method, function, mode and the shim-mode external form, with the rules the parser enforces."
weight = 30
+++

Directives are ordinary line comments read by `cppgo gen`. A package-level
directive (`library`, `header`, `mode`) may appear anywhere; a binding directive
(`ctor`, `dtor`, `method`, `function`, `external`) must be **in the doc comment
of the Go function it binds**.

```go
//cppgo:library libmathx
//cppgo:header include/mathx/vector.hpp

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)
```

The syntax is `//cppgo:<name>` optionally followed by **one** space and one
argument. The argument is the rest of the line, trimmed; `library`, `header`,
`method` and `function` reject arguments containing spaces, so a path with a
space in it cannot be expressed and has to be reached another way.

## Package directives

### `//cppgo:library <name>`

The shared object to open, once per package. It is passed to `dlopen` **as
written**: `libmathx` relies on the loader's search path, while
`./build/libmathx.so` is a path.

The generated package exposes a variable that overrides it at run time:

```go
mathx.CppgoLibraryPath = "/opt/mathx/lib/libmathx.so"
```

It is read once, on the first call. A missing library or symbol is reported then,
not at generation time.

`//cppgo:library` is required. Without it the generator stops with
`missing //cppgo:library <name> directive`.

### `//cppgo:header <path>`

The C++ header whose declarations are bound. One per package, resolved relative
to the package directory. Its presence selects the **direct ABI mode**.

The header is parsed together with the translation units that include it — the
header itself, source files in its directory, and entries in the compilation
database — because a declaration alone does not say whether the member has an
out-of-line definition. That is how the generator distinguishes
`Vector::norm()` (defined in a `.cpp`, has a symbol) from `Vector::x()` (defined
inline, has none).

### `//cppgo:mode abi|shim`

Chooses the calling convention for the whole package.

| mode | selected by | calls |
|---|---|---|
| `abi` | `//cppgo:mode abi`, or simply a `//cppgo:header` | the mangled C++ symbol, with `this` in the first integer slot |
| `shim` | `//cppgo:mode shim`, or no header at all | an `extern "C"` entry point you implement against `cppgo_shim.h` |

Rules the parser enforces:

- `//cppgo:mode shim` together with `//cppgo:header` is an error: *"the direct
  ABI mode, which //cppgo:mode shim opts out of: remove one of the two"*.
- `//cppgo:mode abi` without `//cppgo:header` is an error: there is nothing to
  resolve or validate against.
- A duplicate `mode` directive is an error.

### `//cppgo:class <qualified C++ name>`

Declares that a Go type mirrors a C++ class. It must sit on a single,
non-generic type declaration whose underlying type is exactly `cppgo.Handle`:

```go
//cppgo:class mathx::Vector
type Vector cppgo.Handle
```

The receiver value *is* the object pointer, so there is no field convention to
remember and no wrapper struct to allocate. `//cppgo:class` is an ABI-mode
directive: in shim mode it is rejected. Two Go types cannot mirror the same C++
class, and one Go type cannot mirror two.

## Binding directives

### `//cppgo:ctor`

Binds a constructor. The Go function must **not** be a method, and its first
result must be the class-handle type:

```go
//cppgo:ctor
func New(x, y float64) (Vector, error)
```

The result list may be just `Vector`. The class is taken from that result type,
so the constructor does not need a receiver. The generator allocates
`sizeof(Vector)` bytes (and honours `alignof`) in Go storage, keeps the buffer
reachable in a package registry, calls the constructor with that address as
`this`, and returns the handle. The destructor releases the registry entry.

That allocation is why a constructor needs the header's real size: it is
measured by parsing a probe translation unit that includes the header, not
guessed from the Go type.

### `//cppgo:dtor`

Binds a destructor. It must be a method, take **no arguments** and return
**nothing**:

```go
//cppgo:dtor
func (v Vector) Destroy()
```

It calls the destructor (`D1`) with the receiver as `this`, then drops the
storage the matching constructor allocated. Calling it twice is a
use-after-free on the C++ side, exactly as in C++; nothing in Go prevents it.

### `//cppgo:method <name>`

Binds a non-static member function. It must be a method whose receiver is a
`//cppgo:class` type, and the argument is a **single member name** — no `::`, no
parameter list:

```go
//cppgo:method norm
func (v Vector) Norm() float64

//cppgo:method dot
func (v Vector) Dot(ys []float64) (float64, error)
```

The C++ entity is `mathx::Vector::dot`, resolved against the receiver's class.
Overloads are disambiguated by the Go signature: the candidate must take the
same number of C++ parameters (a Go string or slice counts as two: pointer and
length) and each parameter must accept the corresponding Go type. If more than
one overload fits, the generator refuses: *"N overloads accept the Go argument
types; rename the Go function or drop the binding so the call is unambiguous"*.

### `//cppgo:function <qualified name>`

Binds a free function or a **static** member function. It must not be a method:

```go
//cppgo:function mathx::saxpy
func Saxpy(alpha float64, x, y []float64) int32

//cppgo:function mathx::Vector::dims
func Dims() int32
```

The argument is the qualified C++ name. A static member has no `this`; binding
it with `//cppgo:method` produces *"it is a static member function, which has no
this: bind it with //cppgo:function ..."*.

### `//cppgo:external <symbol>` (shim mode only)

The v1 form: binds an `extern "C"` entry point by its plain C symbol name, which
must be a valid C identifier.

```go
//cppgo:library libmathx
//cppgo:mode shim

//cppgo:external add_i64
func AddI64(a, b int64) int64
```

In shim mode the generator also writes `cppgo_shim.h`, the prototypes your C++
side must implement. `//cppgo:external` on a method is rejected, and the
directive is rejected outright in ABI mode: *"selects the shim mode; in abi mode
use //cppgo:ctor, //cppgo:method, //cppgo:dtor or //cppgo:function"*.

## What each directive is allowed to bind

| directive | receiver | result | mode |
|---|---|---|---|
| `ctor` | none | first result must be the class handle | abi |
| `dtor` | required | nothing | abi |
| `method` | required, named | value and/or trailing `error` | abi |
| `function` | none | value and/or trailing `error` | abi |
| `external` | none | value and/or trailing `error` | shim |

A generic function cannot be bound in either mode. Variadic functions cannot be
bound: pass a slice. Parameter names may be omitted or `_`; the generator
renames them in the rewritten source so the delegating body can pass them.

## Not implemented

Design documents describe two more tiers of ABI discovery and a
`//cppgo:abi` directive for writing the calling convention down by hand. **They
are designed but not implemented.** This version resolves bindings only through
libclang and the header (tier 1); it does not read `.dynsym`, does not demangle,
and does not accept `//cppgo:abi`. If you need a declaration libclang cannot see,
the supported answer today is the shim mode. See [limitations and
roadmap]({{< ref "project/limitations.md" >}}).
