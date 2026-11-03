package cppgo_test

import (
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
	"github.com/stretchr/testify/assert"
)

func TestHandleIsNil(t *testing.T) {
	var zero cppgo.Handle
	assert.True(t, zero.IsNil(), "the zero handle is the null pointer")

	h := cppgo.Handle(0xdead_beef)
	assert.False(t, h.IsNil())
	assert.Equal(t, uintptr(0xdead_beef), uintptr(h))
}

// The conversions below are the contract generated code relies on: a Handle
// must round-trip through unsafe.Pointer without a helper.
func TestHandleRoundTripsThroughUnsafePointer(t *testing.T) {
	buf := make([]byte, 8)
	ptr := unsafe.Pointer(&buf[0])

	h := cppgo.Handle(uintptr(ptr))
	assert.False(t, h.IsNil())
	assert.Equal(t, ptr, unsafe.Pointer(uintptr(h)))
}

func TestHandleZeroIsNotASpecialCaseOfUintptr(t *testing.T) {
	// Guard against someone "improving" Handle into a struct later: the
	// generated code converts it directly to uintptr.
	var h cppgo.Handle
	assert.Equal(t, unsafe.Sizeof(uintptr(0)), unsafe.Sizeof(h))
}
