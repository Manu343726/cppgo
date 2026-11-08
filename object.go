package cppgo

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Object is storage for a C++ value whose layout Go cannot see.
//
// The generator creates one per opaque bound record. Its address stays the same
// for as long as any reference to the object is held, and the C++ side treats
// that address as the value's this. The reference count is Go's: Retain lets
// several Go values share the object and Release drops one reference, and the
// destructor the object was created with runs exactly once, when the last
// reference goes.
//
// The zero Object is unusable and an Object is never copied; hold a *Object and
// get one from NewObject. Addr, Retain and Release are safe for concurrent use
// once the object exists, and Retain must not race with the last Release, which
// would be a use-after-free.
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

// NewObject allocates size bytes of zeroed storage aligned to align, and returns
// an object whose reference count is one.
//
// release is the destructor and runs exactly once, at the last Release: it calls
// the C++ destructor at Addr for a value constructed into this storage, or
// deletes what C++ allocated for it. A nil release means the storage needs no
// destructor.
//
// An align of 0 is treated as 1. A size of 0 still yields a valid non-nil
// address, because C++ allows empty objects and that address is the this they
// see.
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

// Addr returns the address of the storage, or 0 for a nil object. The address
// stays valid until the last reference is released.
func (o *Object) Addr() uintptr {
	if o == nil {
		return 0
	}
	return o.addr
}

// Retain adds one reference, which is what a view does when it shares ownership
// with the object it was derived from. Retaining a nil object is a no-op.
func (o *Object) Retain() {
	if o == nil {
		return
	}
	o.refs.Add(1)
}

// Release drops one reference. The last one runs the object's release function
// and makes its address invalid.
//
// Releasing a nil object is a no-op, and a count that is already zero has no
// further effect, so the destructor never runs twice.
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
