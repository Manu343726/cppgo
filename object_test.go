package cppgo

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewObjectAlignment checks that the address handed to C++ honours the
// requested alignment and that the storage starts zeroed (C++ constructs into
// it, but a constructor is free to read before it writes nothing; zeroed is the
// only predictable value).
func TestNewObjectAlignment(t *testing.T) {
	cases := []struct {
		name  string
		size  uintptr
		align uintptr
	}{
		{"byte", 1, 1},
		{"sixteen-eight", 16, 8},
		{"odd-size", 3, 8},
		{"cache-line", 64, 64},
		{"zero-align", 8, 0},
		{"zero-size", 0, 8},
		{"over-aligned", 24, 32},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := NewObject(tc.size, tc.align, nil)
			defer o.Release()

			align := tc.align
			if align == 0 {
				align = 1
			}
			require.NotZero(t, o.Addr())
			assert.Zero(t, o.Addr()%align, "address %#x is not %d-aligned", o.Addr(), align)
			// The address lives inside the pinned storage.
			base := uintptr(unsafe.Pointer(&o.storage[0]))
			assert.GreaterOrEqual(t, o.Addr(), base)
			assert.Less(t, o.Addr(), base+uintptr(len(o.storage)))
			for _, b := range o.storage {
				assert.Zero(t, b, "storage is not zeroed")
			}
		})
	}
}

// TestObjectRetainRelease walks the reference count and asserts the release
// function runs exactly once, at the last reference.
func TestObjectRetainRelease(t *testing.T) {
	var releases atomic.Int32
	o := NewObject(8, 8, func() { releases.Add(1) })

	o.Retain()
	o.Retain()
	o.Release()
	assert.Equal(t, int32(0), releases.Load(), "release ran before the last reference")
	o.Release()
	assert.Equal(t, int32(0), releases.Load(), "release ran before the last reference")
	o.Release()
	assert.Equal(t, int32(1), releases.Load(), "release did not run at the last reference")
}

// TestObjectReleaseExactlyOnce asserts that an extra Release after the count
// reached zero does not run the destructor a second time.
func TestObjectReleaseExactlyOnce(t *testing.T) {
	var releases atomic.Int32
	o := NewObject(8, 8, func() { releases.Add(1) })
	o.Release()
	o.Release()
	o.Release()
	assert.Equal(t, int32(1), releases.Load())
	assert.Zero(t, o.Addr(), "a released object keeps handing out an address")
	assert.Nil(t, o.storage, "a released object keeps its storage alive")
}

// TestObjectNilRelease covers the storage-only object: no destructor, and the
// release still unpins and forgets the address.
func TestObjectNilRelease(t *testing.T) {
	o := NewObject(8, 8, nil)
	require.NotZero(t, o.Addr())
	o.Release()
	assert.Zero(t, o.Addr())
}

// TestObjectNilReceiver documents that the three methods are safe on a nil
// Object, which is what a zero generated value holds.
func TestObjectNilReceiver(t *testing.T) {
	var o *Object
	assert.Zero(t, o.Addr())
	o.Retain()
	o.Release()
}

// TestObjectConcurrentRetainRelease exercises the reference count from many
// goroutines. Run with -race: the count is atomic and the destructor must still
// run once.
func TestObjectConcurrentRetainRelease(t *testing.T) {
	var releases atomic.Int32
	o := NewObject(16, 8, func() { releases.Add(1) })

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			o.Retain()
			o.Release()
		}()
	}
	wg.Wait()
	o.Release()
	assert.Equal(t, int32(1), releases.Load())
}

// TestObjectPinnedAcrossGarbageCollection forces collections while the object
// exists and checks the address does not change, which is what the pinner is
// for.
func TestObjectPinnedAcrossGarbageCollection(t *testing.T) {
	o := NewObject(32, 8, nil)
	defer o.Release()
	before := o.Addr()

	keep := make([]*Object, 0, 64)
	for i := 0; i < 64; i++ {
		keep = append(keep, NewObject(8, 8, nil))
	}
	runtime.GC()
	runtime.GC()

	assert.Equal(t, before, o.Addr())
	for _, object := range keep {
		object.Release()
	}
}
