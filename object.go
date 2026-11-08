package cppgo

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Object is storage for a C++ value whose layout Go cannot see.
//
// The generator creates one for every opaque bound record. It is a Go
// allocation, aligned and pinned so its address stays put, handed to the
// C++ side as this. The reference count is Go's. Retain lets several Go values
// share one object, and Release drops one reference. The concrete destructor
// the object was created with runs exactly once, when the last reference goes.
//
// The zero Object is unusable, and Object values are never copied. Always hold
// a *Object. NewObject returns one. Addr, Retain and Release are safe
// for concurrent use once the object exists. Retain must not race with the
// last Release, because that would be a use-after-free by another name.
type Object struct {
	// pinner keeps the storage from moving while C++ holds its address. It is
	// the mechanism, not a policy: the collector is free to move anything it
	// does not know is pinned, and the C++ side is not traced.
	pinner runtime.Pinner
	// storage is the allocation the pinned address points into. Keeping the
	// slice reachable gives the collector something to trace; a bare uintptr
	// would not.
	storage []byte
	// addr is the aligned address the C++ side sees.
	addr uintptr
	// refs counts the Go-side values sharing the object.
	refs atomic.Int64
	// release is the concrete destructor, run at the last reference.
	release func()
}

// NewObject allocates size bytes of zeroed storage aligned to align, pins it,
// and returns an object whose reference count starts at one.
//
// release runs exactly once, when the last reference is released. It is the
// concrete destructor. A value constructed into this storage gets the C++
// destructor called at Addr. A value the shim heap-allocated is deleted.
// A nil release is allowed and means the storage needs no destructor.
//
// An align of 0 is treated as 1, and a size of 0 still yields a valid, non-nil
// address, because C++ permits empty objects and the address is what `this`
// is.
func NewObject(size, align uintptr, release func()) *Object {
	if align == 0 {
		align = 1
	}
	if size == 0 {
		size = 1
	}
	// One extra alignment-1 bytes is enough to find an aligned address inside
	// the allocation whatever the allocator returns.
	storage := make([]byte, size+align-1)
	o := &Object{storage: storage, release: release}
	// Pin the first byte: the pinner holds the whole allocation in place.
	o.pinner.Pin(&storage[0])
	base := uintptr(unsafe.Pointer(&storage[0]))
	o.addr = base + (align-base%align)%align
	o.refs.Store(1)
	return o
}

// Addr returns the pinned, aligned address of the storage, or 0 for a nil
// object. The address stays valid until the last reference is released.
func (o *Object) Addr() uintptr {
	if o == nil {
		return 0
	}
	return o.addr
}

// Retain adds one reference. It is what a view does when it shares ownership
// with the object it was derived from.
func (o *Object) Retain() {
	if o == nil {
		return
	}
	o.refs.Add(1)
}

// Release drops one reference. When the last one goes, the object's release
// function runs and the storage is unpinned.
//
// Releasing a nil object is a no-op. Releasing an object whose count is already
// zero has no further effect. The destructor has run and is never run twice.
func (o *Object) Release() {
	if o == nil {
		return
	}
	if o.refs.Add(-1) != 0 {
		return
	}
	if o.release != nil {
		o.release()
	}
	o.pinner.Unpin()
	o.storage = nil
	o.addr = 0
}
