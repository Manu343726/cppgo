package cppgo_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// openFixture loads the test fixture and closes it when the test ends.
func openFixture(t *testing.T, v testlib.Variant) *cppgo.Library {
	t.Helper()

	lib, err := cppgo.Open(testlib.Build(t, v))
	require.NoError(t, err)
	require.NotNil(t, lib)
	t.Cleanup(func() { _ = lib.Close() })

	return lib
}

// fn resolves a fixture symbol to the address the trampoline expects.
func fn(t *testing.T, lib *cppgo.Library, name string) unsafe.Pointer {
	t.Helper()

	addr, err := lib.Symbol(name)
	require.NoError(t, err, "symbol %s", name)
	require.NotZero(t, addr, "symbol %s", name)

	return rtl.Pointer(addr)
}

// callInt calls a fixture function that returns an integer.
func callInt(t *testing.T, f unsafe.Pointer, i [6]uintptr) int64 {
	t.Helper()

	cppgo.ProbeStack()
	raw, _ := asm.Call(f, &asm.Args{I: i})

	return int64(raw)
}

// callFloat calls a fixture function that returns a double.
func callFloat(t *testing.T, f unsafe.Pointer, i [6]uintptr, fl [8]float64) float64 {
	t.Helper()

	cppgo.ProbeStack()
	_, raw := asm.Call(f, &asm.Args{I: i, F: fl})

	return raw
}

// panicValue runs f and returns what it panicked with, or nil.
func panicValue(t *testing.T, f func()) (value any) {
	t.Helper()

	defer func() { value = recover() }()
	f()

	return nil
}

// lockThread pins the calling goroutine to its OS thread for the rest of the
// test.
//
// The fixture stores its error message in thread-local storage, like the real
// cppgo_last_error contract. A goroutine that is preempted between the call
// that records the error and the call that reads it back can resume on another
// thread, where it would find another thread's message; pinning the goroutine
// is what makes that window impossible in a test.
func lockThread(t *testing.T) {
	t.Helper()

	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
}
