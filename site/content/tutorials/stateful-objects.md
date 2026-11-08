+++
title = "Stateful objects"
description = "Constructors, Close and where a C++ object lives: the generated NewVector and Close, the cppgo.Object behind them, copying versus Clone, and why closing twice is safe."
weight = 20
+++

A free function has no state. A class does, and state means someone must decide
where the object lives and when it dies. cppgo decides the first: the generated
`NewVector` allocates Go storage, pins it, and hands the address to the C++
constructor. You decide the second: `Close`.

**Audience:** you have called a free function and now need a class. This page
extends [the first tutorial]({{< ref "tutorials/first-binding.md" >}}) with a
`Vector` whose header is the one in the
[quickstart]({{< ref "manual/quickstart.md" >}}).

## What you generate

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
```

The package contains a Go type and its methods:

```go
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Close() error
func (v *Vector) Clone() (*Vector, error)
func (v *Vector) Dot(ys []float64) (float64, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
```

Four things to notice:

- **`NewVector` is a function, not a method.** A constructor has no receiver; the
  generated name is always `New<Type>`.
- **`Vector` is opaque.** Its only field is unexported (`obj *cppgo.Object`), so
  a Go caller cannot reach into the C++ layout — and cannot accidentally copy
  the object by copying a struct value.
- **`Scale` has no error, `Norm` has one.** `scale` is `noexcept` in the header,
  so its call is direct and cannot fail; `norm` is not, so it goes through a
  thunk that can catch an exception.
- **`Close` is the destructor**, named once and defined by the generated
  package.

## Where the object lives

`NewVector` allocates `sizeof(Vector)` bytes — measured from the header by
libclang, 16 here — and pins them:

```go
obj = cppgo.NewObject(16, 8, func() {
	if !constructed {
		return
	}
	cppgo.ProbeStack()
	cppgoCall_06a8bef46cac(cppgoSym_06a8bef46cac, obj.Addr())
})
```

The release function is the destructor: it runs when the object's last
reference goes. The `constructed` flag is what stops it from running on an
object whose constructor threw. The full generated body and the reference count
are in [object lifetimes]({{< ref "manual/lifetimes.md" >}}).

## Call it

```go
v, err := mathx.NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

n, err := v.Norm() // 5
if err != nil {
	log.Fatal(err)
}
fmt.Println(n)

v.Scale(2)

n, err = v.Norm() // 10
if err != nil {
	log.Fatal(err)
}
fmt.Println(n)
```

```console
$ go run .
5
10
```

`NewVector` ran the C++ constructor in Go storage — the equivalent of a
placement `new`. `Norm` read the object through its pointer. `Scale` mutated it
in place; no allocation happened on any of the three calls.

## Close is safe to call twice

`Close` does not free C++ memory twice. It drops the object reference and
reports that the value is already closed:

```go
if err := v.Close(); err != nil {
	log.Fatal(err) // nil: the object was live
}
if err := v.Close(); err != nil {
	fmt.Println(err) // cppgo: ErrClosed
}
```

A method called on a closed or nil `*Vector` returns `cppgo.ErrClosed` as well,
which is why `Close` can be deferred without a nil check and why a method after
`Close` is a Go error instead of a segfault.

## Copying is not cloning

The Go value is a pointer to one object. Copying the pointer copies the
reference, not the C++ vector:

```go
a, _ := mathx.NewVector(3, 4)
defer a.Close()
b := a // same object
b.Scale(2)
n, _ := a.Norm()
fmt.Println(n) // 10
```

`Clone()` is the copy constructor: a second object in fresh storage, with its own
`Close`.

```go
clone, err := a.Clone()
if err != nil {
	return err
}
defer clone.Close()
```

<div class="callout callout-warn">
<span class="callout-title">There is no finalizer</span>
<p>
An object you never close is never destroyed. That is deliberate: a finalizer
runs at an unpredictable time, on an arbitrary goroutine, possibly while the
library is unloading. <code>defer v.Close()</code> immediately after a successful
construction is the idiom, exactly as <code>delete</code> follows
<code>new</code> in C++.
</p>
</div>

## When construction can fail

`NewVector` returns `(*Vector, error)` because the constructor is not
`noexcept`. A C++ constructor that throws is caught by the generated thunk, the
storage is released, and no half-built object is returned. The exception on the
Go side is `*cppgo.CallError` naming `mathx::Vector::Vector`.

## Where to go next

`Norm()` returns one `double`. Real code passes buffers. [Bulk
data]({{< ref "tutorials/bulk-data.md" >}}) explains how a `[]float64` crosses,
what is copied and what is borrowed, and why the answer is not "call the method
once per element".
