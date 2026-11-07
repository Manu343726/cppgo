+++
title = "Object lifetimes"
description = "Handles carry no ownership, every constructor needs its destructor, where the object actually lives in ABI mode, and what happens when a library is closed while a call is still running."
weight = 70
+++

## A handle is a number, not an owner

```go
type Handle uintptr
```

`Handle` is a `uintptr` rather than a pointer on purpose: the memory it refers to
is owned by the foreign side (or by a Go allocation whose layout mirrors it), and
treating it as an `unsafe.Pointer` would invite the garbage collector and `vet`'s
`unsafeptr` check to reason about memory that is not Go's.

The consequences follow directly:

- **Nothing frees the object when the handle becomes unreachable.** There is no
  finalizer, and that is deliberate: a finalizer runs at an unpredictable time
  on an arbitrary goroutine, possibly while the library is closing.
- **The zero handle is null.** `Handle(0).IsNil()` is true. A failing
  constructor is expected to leave it behind; the generated code never
  synthesises a handle.
- **Copying a handle is copying a number.** Two Go values can refer to the same
  C++ object; nothing tracks the reference count.

In the direct ABI mode the handle is also the receiver:

```go
//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)

//cppgo:dtor
func (v Vector) Destroy()
```

so `v` *is* the object pointer, with no field to unwrap. In hand-written code
the common shape is a struct around it:

```go
type Vector struct{ h cppgo.Handle }

func NewVector(x, y float64) (*Vector, error) {
	var out cppgo.Handle
	if err := cppgoCreate(&out, x, y); err != nil {
		return nil, err
	}
	return &Vector{h: out}, nil
}

func (v *Vector) Close() error { return cppgoDestroy(v.h) }
```

Either way the rule is the same: **for every constructor there must be a
matching destroy, and the Go code is responsible for calling it.**

## Where the object lives in ABI mode

A C++ constructor needs memory to construct into. The generated package provides
it:

1. The generator measures `sizeof` and `alignof` of the class by parsing a probe
   translation unit that includes the header. The size is not guessed from the
   Go type, which is only a `uintptr`.
2. The wrapper calls `cppgoAlloc(size, align)`, which allocates a zeroed
   `[]byte`, aligns its start, and records it in a package-level
   `map[cppgo.Handle][]byte`.
3. The constructor is called with that address as `this` — the equivalent of a
   placement `new`.
4. The destructor calls `D1` with the same address and then deletes the map
   entry, which is what lets the collector reclaim the buffer.

The registry exists because a `Handle` is a `uintptr` and the collector does not
trace it. Without the map, the buffer could be reclaimed while C++ still held a
pointer into it. The entry is keyed by the handle, so the address stays stable
for the object's whole life.

<div class="callout callout-warn">
<span class="callout-title">An object you never destroy is never reclaimed</span>
<p>
The registry entry keeps the buffer alive forever. That is the same rule as C++
<code>new</code> without <code>delete</code>: not a leak the runtime can fix,
but a leak you wrote. It is why the destructor is not optional, and why
<code>defer v.Destroy()</code> belongs immediately after a successful
construction.
</p>
</div>

## Destruction order, and closing the library

`Library.Close` unloads the shared object. Every later `Symbol` call fails with
`ErrClosed`, and so does a second `Close`. What `Close` **cannot** do is wait for
calls that are already running inside the library: there is no reference count
per call, and a program that closes a library while another goroutine is in one
of its functions is asking for a use-after-free.

The safe shape is therefore:

```go
lib, err := cppgo.Open("libmathx.so")
if err != nil {
	return err
}
defer lib.Close()        // after the work is done, once
```

For generated packages the library is opened once and kept for the life of the
process; `Close` is not exposed. If you need to unload it, you are using the
runtime package directly, and you own the ordering: destroy the objects, stop
the calls, then close.

## Threads and goroutines

- **Calling the same C++ object from several goroutines** is exactly as safe as
  calling it from several C++ threads. cppgo adds no lock. A `const` method that
  only reads is fine; a mutating method needs the same synchronisation it would
  need in C++.
- **Resolving symbols** from several goroutines is safe: `Library.Symbol` takes
  a read lock, and `Close` is serialised against it.
- **The error message is thread-local.** `cppgo_last_error` is per-thread, and
  a goroutine is not pinned to an OS thread. The generated wrapper reads it
  immediately after the failing call, on the same goroutine, before anything
  could migrate — but if you hand-write a binding, keep the status check and
  `LastError` adjacent.

## A destructor is not a finalizer

There is no idempotence guarantee. Calling the generated destructor twice calls
`D1` on freed storage, which is a use-after-free; calling it with a zero handle
calls `D1` on address 0. Both are the caller's mistake, and both are cheap to
avoid:

```go
v, err := mathx.New(3, 4)
if err != nil {
	return err
}
defer v.Destroy()
```

If the object has to outlive the function, keep it in a struct and give that
struct a `Close` that is documented as call-once, which is what the runtime
package's `Handle` documentation recommends.

Next: [validation]({{< ref "manual/validation.md" >}}).
