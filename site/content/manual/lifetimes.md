+++
title = "Object lifetimes"
description = "Where a C++ object lives when Go creates it, why every constructor has a Close, what cppgo.Object pins and reference-counts, and what happens when an object is closed twice or a library is unloaded."
weight = 70
+++

**Audience:** your first C++ object that lives longer than one call.

## The generated type owns one object

A class becomes a Go struct with unexported storage:

```go
// Vector is mathx::Vector. The object belongs to C++; obj is where it lives.
type Vector struct {
	obj *cppgo.Object
}
```

`cppgo.Object` is the Go allocation the C++ value lives in. It pins the storage
with `runtime.Pinner`, so the collector cannot move it while C++ holds the
address, and it counts references so a view can share the object without a
second destructor. A generated constructor creates one:

```go
func NewVector(x float64, y float64) (*Vector, error) {
	if err := cppgoOpen(""); err != nil {
		return nil, err
	}
	cppgo.ProbeStack()
	constructed := false
	var obj *cppgo.Object
	obj = cppgo.NewObject(16, 8, func() {
		if !constructed {
			return
		}
		cppgo.ProbeStack()
		cppgoCall_06a8bef46cac(cppgoSym_06a8bef46cac, obj.Addr())
	})
	if cppgoStatus := cppgoCall_7d8c482bce3e(cppgoSym_7d8c482bce3e, obj.Addr(), x, y); cppgoStatus != 0 {
		obj.Release()
		return nil, cppgoCallError("mathx::Vector::Vector")
	}
	constructed = true
	return &Vector{obj: obj}, nil
}
```

The `16` and `8` are `sizeof` and `alignof` measured from the header by
libclang, not guessed from the Go type. The release function is the destructor:
it runs exactly once, when the last reference goes — and the `constructed` flag
is what keeps it from running on an object whose constructor threw.

## `Close` is the destructor

```go
func (v *Vector) Close() error {
	if v == nil || v.obj == nil {
		return cppgo.ErrClosed
	}
	obj := v.obj
	v.obj = nil
	obj.Release()
	return cppgoCloseError("mathx::Vector::~Vector")
}
```

- **A second `Close`, a nil receiver, or the zero value returns
  `cppgo.ErrClosed`.** It does not run the destructor twice.
- **`Close` can report an error**: a destructor is not `noexcept` in C++ unless
  it says so, so a throw is caught by the shim and returned here.
- **There is no finalizer.** The object dies when Go calls `Close`, not when the
  collector notices that the `*Vector` is unreachable. `defer v.Close()` belongs
  immediately after a successful construction.

<div class="callout callout-warn">
<span class="callout-title">An object you never close is never released</span>
<p>
The <code>*cppgo.Object</code> keeps the C++ object alive, and the C++ object
may own memory of its own. That is the same rule as C++ <code>new</code> without
<code>delete</code>: not a leak the runtime can fix, but a leak you wrote.
</p>
</div>

## Copying and views

`Clone()` is the copy constructor. It constructs a second object in fresh
storage and returns its own `*Vector`; closing one does not close the other.

```go
clone, err := v.Clone()
if err != nil {
	return err
}
defer clone.Close()
```

A **view** is a second Go value sharing one object. `cppgo.Object.Retain` adds a
reference and `Release` drops one; the destructor runs at the last one. The
generated API uses views for the `As<Base>()` conversion, which is part of the
two-phase inheritance work and is **not emitted yet**: the emitter refuses
inheritance today, so there is no `As` in a generated package.

Copying the Go struct is not a copy of the C++ object: `Vector` is a pointer to
one `cppgo.Object`, and the object is shared.

## Mirrored records are values

A mirrored POD is a plain Go struct with no object behind it:

```go
type Point struct {
	X float64
	Y float64
}
```

`Point` needs no `Close`: it is its own storage, and `NewPoint()` returns a zero
value. Passing it by value copies the bytes, exactly as in C++. The generated
package adds a `Clone()` that copies the fields, which is a convenience, not an
ownership rule.

## The library is loaded once

A generated package loads its two shared objects on the first binding call and
keeps them for the life of the process. There is no generated `Close` for a
library; if you need to unload one, you are using the runtime package directly,
and you own the ordering: destroy the objects, stop the calls, then close.

`cppgo.Library.Close` unloads the object. Every later `Symbol` call fails with
`cppgo.ErrClosed`, and so does a second `Close`. What `Close` **cannot** do is
wait for calls already running inside the library: there is no per-call
reference count, and closing while another goroutine is in one of its functions
is a use-after-free.

## Threads and goroutines

- **Calling the same C++ object from several goroutines** is exactly as safe as
  calling it from several C++ threads. cppgo adds no lock. A `const` method that
  only reads is fine; a mutating method needs the synchronisation it would need
  in C++.
- **Resolving symbols** from several goroutines is safe: `Library.Symbol` takes
  a read lock and `Close` is serialised against it.
- **The error message is thread-local.** `cppgo_last_error` is per-thread, and a
  goroutine is not pinned to an OS thread. The generated wrapper reads it
  immediately after the failing call, on the same goroutine; a hand-written
  binding must keep the status check and `LastError` adjacent.

Next: [validation]({{< ref "manual/validation.md" >}}).
