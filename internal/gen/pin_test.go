package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallbackCallPinsGoMemory proves what a callback-capable call does with the
// Go values it hands to C: each one is copied into a heap object the call pins
// for its duration, because the callback runs Go code that may grow the
// goroutine's stack and move everything in it.
//
// The copies are what make the shapes legal at all: a slice parameter, a
// writable record and a text prefix used to be refused on a call that takes a
// callback, and the refusal named the stack growth as the reason.
func TestCallbackCallPinsGoMemory(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "payloads", "payloads.hpp")},
		Package: "payloads",
		Library: "libpayloads",
		Out:     ".",
	})
	require.NoError(t, err)
	for _, warning := range rendered.Warnings {
		assert.NotContains(t, warning, "not bound", "the Go pointer shapes bind on a callback-capable call")
	}

	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "\"runtime\"", "pinning a heap object needs the runtime's Pinner")
	assert.Contains(t, goFile, "cppgoPin := new(runtime.Pinner)")
	assert.Contains(t, goFile, "defer cppgoPin.Unpin()")

	// The const slice: a pinned copy in, and nothing copied back.
	assert.Contains(t, goFile, "cppgoPinArg0 := cppgoPinSlice(cppgoPin, data)")

	// The writable slice and record: a pinned copy, and the callee's writes go
	// back into the caller's value.
	assert.Contains(t, goFile, "cppgoPinArg0 := cppgoPinSlice(cppgoPin, data)")
	assert.Contains(t, goFile, "copy(data, cppgoPinArg0)")
	assert.Contains(t, goFile, "cppgoPinArg0 = cppgoPinValue(cppgoPin, *point)")
	assert.Contains(t, goFile, "*point = *cppgoPinArg0")

	// The text prefix: the NUL-terminated copy is pinned where it stands.
	assert.Contains(t, goFile, "cppgoPin.Pin(&cppgo_prefix_cstr_keep[0])")

	// The result cell: a heap cell the thunk writes through after the callback
	// returned, pinned for the same reason.
	assert.Contains(t, goFile, "cppgoResult := cppgoHeapCell[int32]()")
	assert.Contains(t, goFile, "cppgoPin.Pin(cppgoResult)")

	// The helpers are functions with the noinline directive: a cell that only
	// reaches a uintptr is stack allocated, and pinning a stack object does not
	// stop the stack from being copied.
	for _, helper := range []string{"func cppgoHeapCell[T any]() *T", "func cppgoPinValue[T any](", "func cppgoPinSlice[T any]("} {
		assert.Contains(t, goFile, helper)
	}
	assert.Contains(t, goFile, "//go:noinline\n"+`func cppgoHeapCell`)
}
