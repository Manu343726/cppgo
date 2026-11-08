package shim_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
)

// TestDirectCarrierOnTheUsersLibrary measures the fast path on the same fixture
// the shim rows use: an exported, out-of-line, non-virtual function is called
// by its mangled name, with no shim involved at all.
func TestDirectCarrierOnTheUsersLibrary(t *testing.T) {
	f := build(t)
	asm.Probe()

	lib, err := openUser(t, f)
	require.NoError(t, err)

	addr, err := lib.Symbol("_ZN4user11checked_divEii")
	require.NoError(t, err, "the user's library exports the out-of-line function")

	var a asm.Args
	a.I[0] = 42
	a.I[1] = 2
	got, _ := asm.Call(rtlPointer(addr), &a)
	assert.EqualValues(t, 21, int32(got))
}

// TestVirtualDispatchFromTheVTable measures the other half of row 1: dispatch
// can also be done from Go, without the shim, by reading the object's vptr and
// calling a slot. The slot index is the Itanium layout's declaration order
// (offset-to-top, typeinfo, then the destructors, then the virtual functions),
// so it is generation-time knowledge, not runtime discovery.
func TestVirtualDispatchFromTheVTable(t *testing.T) {
	f := build(t)
	asm.Probe()

	var circle unsafe.Pointer
	var newCircle asm.Args
	newCircle.F[0] = 2
	newCircle.I[0] = ptr(unsafe.Pointer(&circle))
	call(t, f, "shim_circle_new", &newCircle)
	require.NotNil(t, circle)

	// The vptr is the object's first word.
	vptr := *(*uintptr)(circle)
	require.NotZero(t, vptr)

	// The vptr points at the address point: the offset-to-top and the
	// typeinfo come *before* it. Layout for user::Shape: [0] the complete
	// destructor, [1] the deleting destructor, [2] area, [3] name. Measured
	// with a C++ dump of the same vtable.
	area := *(*uintptr)(unsafePointerFrom(vptr + 2*8))
	require.NotZero(t, area)

	var a asm.Args
	a.I[0] = ptr(circle)
	_, got := asm.Call(unsafePointerFrom(area), &a)
	assert.Equal(t, 12.0, got, "slot 2 of the vtable is Circle::area")

	// A Disk's primary vtable is Serializable's, not Shape's, because
	// Serializable is its first base; the Shape sub-vtable is reached
	// through the adjusted pointer. This is why the Go side never computes
	// an offset itself.
	var disk unsafe.Pointer
	var newDisk asm.Args
	newDisk.I[0] = 1
	newDisk.F[0] = 3
	newDisk.I[1] = ptr(unsafe.Pointer(&disk))
	call(t, f, "shim_disk_new", &newDisk)

	diskVptr := *(*uintptr)(disk)
	assert.NotEqual(t, vptr, diskVptr, "the primary vtable is the first base's")
}
