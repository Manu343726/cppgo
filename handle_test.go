package cppgo_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"

	"github.com/Manu343726/cppgo"
)

func TestHandleIsNil(t *testing.T) {
	var zero cppgo.Handle
	assert.True(t, zero.IsNil(), "the zero handle is the null pointer")

	h := cppgo.Handle(0xdead_beef)
	assert.False(t, h.IsNil())
	assert.Equal(t, uintptr(0xdead_beef), uintptr(h))
}

// A Handle must be interchangeable with uintptr, because that is exactly what
// generated code does when it hands a receiver to a trampoline:
// `uintptr(v)` goes into RDI as the implicit this argument.
//
// The reverse conversion (uintptr -> unsafe.Pointer) is deliberately NOT
// asserted here: it is the one pattern `go vet`'s unsafeptr checker exists to
// flag, and asserting it in a test would teach the wrong idiom.
func TestHandleConvertsToUintptr(t *testing.T) {
	buf := make([]byte, 8)
	ptr := unsafe.Pointer(&buf[0])

	h := cppgo.Handle(uintptr(ptr))
	assert.False(t, h.IsNil())
	assert.Equal(t, uintptr(ptr), uintptr(h), "the handle must be the object address")
}

func TestHandleHasTheWidthOfUintptr(t *testing.T) {
	// Guard against someone "improving" Handle into a struct later: generated
	// code converts it to uintptr directly.
	var h cppgo.Handle
	assert.Equal(t, unsafe.Sizeof(uintptr(0)), unsafe.Sizeof(h))
}
