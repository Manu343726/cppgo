//go:build stackalloc

package stackalloc

import "unsafe"

// callOut is the emitted accessor's trampoline shape: a body-less declaration
// with no //go:noescape, so out leaks and the result slot escapes to the heap.
// callout_amd64.s implements it.
func callOut(fn uintptr, this uintptr, out unsafe.Pointer) int32

// callOutFloat invokes fn(this, &result) through that trampoline and returns
// the result, which is exactly what a generated accessor does.
//
//go:noinline
func callOutFloat(fn, this uintptr) float64 {
	var result float64
	callOut(fn, this, unsafe.Pointer(&result))
	return result
}
