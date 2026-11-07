+++
title = "mathx: a class"
description = "The direct ABI mode on one small class: out-of-line definitions, C1/D1 constructors, const members, a slice parameter, a static member, and an inline member the generator refuses."
weight = 10
+++

**Repository path:** [`examples/mathx/`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx)

`mathx` is a two-double `Vector` with a deliberately small surface: exactly the
constructs the direct ABI mode supports, plus one it does not, left in place so
the refusal can be reproduced.

```console
$ cd examples/mathx
$ make          # build libmathx.so, generate, test, vet
$ make check    # prove the committed generated files are up to date
$ make inline-demo   # watch cppgo refuse an inline member
```

## The header

The class, with the file's longer comments trimmed:

<span class="filename">examples/mathx/include/mathx/vector.hpp</span>

```cpp
class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    double x() const;
    double y() const;
    double norm() const;

    double dot(const double *ys, std::size_t n) const;

    void scale(double k);

    static int dims();

    // Defined inside the class definition, therefore implicitly inline: the
    // library exports no symbol for it and cppgo refuses to bind it.
    double norm_squared() const { return x_ * x_ + y_ * y_; }

private:
    double x_;
    double y_;
};
```

Every member cppgo binds is **declared here and defined out of line** in
`src/vector.cpp`. That split is the requirement, not a style: a member defined
inside the class body is implicitly inline, so the compiler emits no dynamic
symbol and `dlsym` has nothing to find. `norm_squared` is the counter-example.

The class is 16 bytes and its layout is private. cppgo learns the size from
libclang, not from Go, and hands the constructor that many bytes to construct
into.

## The directives

<span class="filename">examples/mathx/mathx.go</span>

```go
//cppgo:library libmathx
//cppgo:header include/mathx/vector.hpp

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)

//cppgo:dtor
func (v Vector) Destroy()

//cppgo:method x
func (v Vector) X() float64

//cppgo:method y
func (v Vector) Y() float64

//cppgo:method norm
func (v Vector) Norm() float64

//cppgo:method dot
func (v Vector) Dot(ys []float64) float64

//cppgo:method scale
func (v Vector) Scale(k float64)

//cppgo:function mathx::Vector::dims
func Dims() int32
```

(`cppgo gen` has already rewritten this file in the repository: each declaration
above has a one-line body delegating to its wrapper. The directives are what you
write; the bodies are what the generator adds.)

Four decisions worth noticing:

- **`Vector` is a defined type over `cppgo.Handle`**, so the receiver *is* the
  object pointer.
- **`Dot` takes `[]float64`**, not a pointer and a length: the wrapper expands it
  to `(pointer, length)` and the C++ side already declares `const double *` plus
  `std::size_t`.
- **`X`, `Y`, `Norm` and `Dot` are `const`**, and their symbols carry `K`
  (`_ZNK5mathx6Vector1xEv`). `Scale` is not, and does not.
- **`Dims` is a static member**, so it is bound with `//cppgo:function`, not
  `//cppgo:method`: it has no `this`.

## The call site

From [`examples/mathx/mathx_test.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/mathx_test.go):

```go
v, err := New(3, 4)
if err != nil {
	t.Fatalf("New(3, 4): %v", err)
}
defer v.Destroy()

if got := v.Norm(); got != 5 {
	t.Errorf("Norm() = %v, want 5", got)
}

// A slice crosses as (pointer, length): 3*2 + 4*1 = 10.
if got := v.Dot([]float64{2, 1}); got != 10 {
	t.Errorf("Dot([2 1]) = %v, want 10", got)
}

v.Scale(2)
if got := v.X(); got != 6 {
	t.Errorf("X() after Scale(2) = %v, want 6", got)
}
```

The tests also check the lifetime from the outside: `New` adds one entry to the
generated storage registry and `Destroy` removes it, which is the only way to see
the release because the handle is a `uintptr`.

## What it teaches

- **The out-of-line rule, with a working counter-example.** `make inline-demo`
  copies the package to `build/inline-demo/`, appends a binding for
  `norm_squared`, and asserts that `cppgo gen` fails with a message mentioning
  *no symbol*. That is the generator behaving as designed, not an example that
  broke.
- **Constructor and destructor symbols are `C1`/`D1`**, chosen by libclang, never
  by a mangled name guessed in Go. The report prints them.
- **`this` is the first integer argument.** `New(3, 4)` is `this` in `RDI`, `3`
  in `XMM0`, `4` in `XMM1`; `Norm()` is `this` in `RDI` and the result in `XMM0`.
- **A slice costs two registers**, not a copy: `Dot` passes `RSI`/`RDX` after
  `this`.
- **A static member is not a method.** One directive changes the whole shape of
  the call.
- **Objects are independent.** Two `New` calls return two handles and two
  distinct objects; nothing is shared between them.

## Numbers

This page quotes no benchmark numbers for the example, because they depend on
your machine and your compiler. The project-level numbers for the crossing itself
— with the clock caveat — are on [benchmarks]({{< ref "project/benchmarks.md"
>}}). This example's suite is correctness-only; the `stats` example next door is
the one with a benchmark harness:

```console
$ cd examples/mathx
$ make
```

Next: [stats]({{< ref "examples/stats.md" >}}).
