+++
title = "Stateful objects"
description = "Constructors, destructors and handles: where a C++ object lives when Go creates it, why every constructor needs a destructor, and what copying or forgetting a handle actually does."
weight = 20
+++

A free function has no state. A class does, and state means someone must decide
where the object lives and when it dies. In the direct ABI mode, Go provides the
storage and C++ constructs into it; the destructor is the only thing that gives
it back.

We will extend the `mathx` package from [the first
tutorial]({{< ref "tutorials/first-binding.md" >}}) with a `Vector`.

## The C++ class

Every member is defined **out of line**, in the `.cpp`. That is not a style
preference: a member defined inside the class definition is implicitly inline,
emits no dynamic symbol, and cannot be bound.

<span class="filename">include/mathx/vector.hpp</span>

```cpp
#pragma once

#include <cstddef>

namespace mathx {

class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    void scale(double k);            // mutating, non-const
    double norm() const;             // const

private:
    double x_;
    double y_;
};

} // namespace mathx
```

<span class="filename">src/vector.cpp</span>

```cpp
#include "mathx/vector.hpp"

#include <cmath>

namespace mathx {

Vector::Vector(double x, double y) : x_(x), y_(y) {}
Vector::~Vector() {}

void Vector::scale(double k) {
    x_ *= k;
    y_ *= k;
}

double Vector::norm() const { return std::sqrt(x_ * x_ + y_ * y_); }

} // namespace mathx
```

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Iinclude -o build/libmathx.so src/*.cpp
```

## The Go bindings

<span class="filename">bindings/mathx/mathx.go</span>

```go
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ../../../include/mathx/vector.hpp

// Vector mirrors mathx::Vector. The receiver value is the object pointer.
//
//cppgo:class mathx::Vector
type Vector cppgo.Handle

// New constructs a Vector.
//
//cppgo:ctor
func New(x, y float64) (Vector, error)

// Scale multiplies both components by k.
//
//cppgo:method scale
func (v Vector) Scale(k float64)

// Norm returns the Euclidean norm.
//
//cppgo:method norm
func (v Vector) Norm() float64

// Destroy destroys the object.
//
//cppgo:dtor
func (v Vector) Destroy()
```

Three things to notice:

- `New` is a **function**, not a method: a constructor has no receiver. Its first
  result names the class, and that is how the generator knows which C++ class to
  construct.
- `Scale` and `Norm` take the receiver by value, because a `Handle` is a number.
  Mutating through it changes the C++ object, not the Go value.
- `Destroy` takes no arguments and returns nothing. A destructor that declares a
  result is a parse error in the generator, not a silent no-op.

## Generate

```console
$ cppgo gen -dir ./bindings/mathx
```

The report now has a note on the constructor:

```
mathx::Vector::Vector   mangled _ZN5mathx6VectorC1Edd      abi from fallback flags (assumed); note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::norm     mangled _ZNK5mathx6Vector4normEv    abi from fallback flags (assumed)
mathx::Vector::scale    mangled _ZN5mathx6Vector5scaleEd    abi from fallback flags (assumed)
mathx::Vector::~Vector  mangled _ZN5mathx6VectorD1Ev       abi from fallback flags (assumed)
```

`16 bytes, alignment 8` is `sizeof(Vector)`: two `double`s, no vtable, no
padding. The generator measured it by parsing a probe translation unit that
includes the header — it did not guess it from the Go type, which is only a
`uintptr`.

`_ZN5mathx6VectorC1Edd` is the complete-object constructor (`C1`), and
`_ZN5mathx6VectorD1Ev` is the complete-object destructor (`D1`). The `K` in
`_ZNK...normEv` is the `const` qualifier: it changes the mangling, not the call.

## Call it

<span class="filename">main.go</span>

```go
package main

import (
	"fmt"
	"log"

	"example.com/vecdemo/bindings/mathx"
)

func main() {
	mathx.CppgoLibraryPath = "./build/libmathx.so"

	v, err := mathx.New(3, 4)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Destroy()

	fmt.Println(v.Norm()) // 5

	v.Scale(2)
	fmt.Println(v.Norm()) // 10
}
```

```console
$ go run .
5
10
```

## What happens on each call

`New(3, 4)`:

1. `cppgoAlloc(16, 8)` allocates a zeroed `[]byte`, aligns its start to 8, and
   records it in a package-level `map[cppgo.Handle][]byte`.
2. The trampoline is called with `this` in `RDI`, `x` in `XMM0`, `y` in `XMM1`.
   The C++ constructor runs in that Go storage — the equivalent of a placement
   `new`.
3. The handle (the aligned address) is returned as `Vector`.

`v.Norm()` passes `this` in `RDI` and reads `XMM0`; `v.Scale(2)` passes `this` in
`RDI` and `k` in `XMM0`. Neither allocates.

`v.Destroy()` calls `D1` with `this` in `RDI`, then `cppgoRelease` deletes the
map entry, which is what lets the collector reclaim the buffer.

## The handle rules, concretely

**A handle is not an owner.** `Vector` is a `uintptr`. Copying it copies a
number; two Go values can refer to the same object and neither knows.

```go
a, _ := mathx.New(3, 4)
b := a            // same object, two handles
b.Scale(2)
fmt.Println(a.Norm()) // 10: they are the same Vector
```

**Nothing frees the object for you.** There is no finalizer. This leaks:

```go
for i := 0; i < 1_000_000; i++ {
	v, _ := mathx.New(1, 1)
	_ = v // the registry entry and the C++ object stay forever
}
```

**Destroy exactly once.** A second call runs `D1` on freed storage, which is a
use-after-free; a call on a zero handle runs it on address 0. Neither is caught
for you, and neither is recoverable.

**The registry is why the memory survives.** The map holds the `[]byte`; a
`uintptr` in a `Vector` cannot. If you return a `Vector` from a function, the
map entry keeps the C++ object alive; the Go value is only the address.

## When construction can fail

There is no status channel in `New`, so a `(Vector, error)` signature returns a
nil error on success and only reports a failure to *load the library*. A
constructor returns `void` at the ABI level, and the handle is the storage Go
allocated, so "constructed object or error" cannot come back from one direct
call.

When construction is fallible, bind it through the **shim mode**, where the
generated C signature is `int32_t f(..., void **out)`: the shim returns a
non-zero status, stores the handle through the out-pointer, and the wrapper
turns the status into a `(cppgo.Handle, error)` result — the same status
convention described in [error handling]({{< ref "manual/errors.md" >}}). The
alternative is a static factory declared as returning only `error`, which works
when the factory hands the object back some way other than a second return
value.

A constructor that throws is worse than fallible: in the direct ABI mode there
is no shim to catch the exception, so the throw is undefined behaviour. Catch
inside C++, or bind the constructor through the shim mode, where your entry
point can catch and return a status.

## Wrapping the handle in a type

The generated mode makes `Vector` itself the handle. If you prefer an API with a
`Close` method and a nil pointer to signal "no object", wrap it:

```go
type Vec struct{ h mathx.Vector }

func (v *Vec) Close() error {
	if v == nil || v.h.IsNil() {
		return errors.New("vec: already closed")
	}
	v.h.Destroy()
	v.h = 0
	return nil
}
```

That is Go's own pattern, not something cppgo generates; the runtime package's
`Handle` documentation shows the same shape. It buys you an idempotent close at
the cost of one indirection — worth it for a long-lived object, unnecessary for a
`defer`-scoped one.

## Where to go next

`Norm()` returns one `double`. Real code passes buffers. [Bulk
data]({{< ref "tutorials/bulk-data.md" >}}) explains how a `[]float64` crosses,
and why the answer is not "call the method once per element".
