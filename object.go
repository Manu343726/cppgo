package cppgo

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Object is storage a C++ value lives in whose layout Go does not know.
//
// The generator creates one for every opaque bound record: a Go allocation,
// aligned and pinned so its address does not move, handed to the C++ side as
// this. The reference count is Go's, not C++'s: Retain lets several Go values
// share one object, and Release drops one reference; the concrete destructor
// the object was created with runs exactly once, when the last reference goes.
//
// The zero Object is not usable and Object values are never copied: always hold
// a *Object, which is what NewObject returns. Addr, Retain and Release are safe
// for concurrent use once the object exists; Retain must not race with the
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
// concrete destructor: for a value constructed into this storage, it calls the
// C++ destructor at Addr; for a value the shim heap-allocated, it deletes it.
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
// zero does nothing more: the destructor has run and is never run twice.
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
