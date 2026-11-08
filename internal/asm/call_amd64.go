//go:build amd64

package asm

import "unsafe"

// Call invokes the foreign function at fn with the arguments in a.
//
// The integer result comes back in RAX and the floating point result in XMM0,
// and a caller reads whichever one its signature names. When a signature has
// both, the integer result is the RAX value.
//
// a must not be nil: Call panics on it. fn is not checked, and a nil one faults.
// Call never grows the Go stack, so the stack guarantee is the caller's: run
// cppgo.ProbeStack from the same goroutine first, and before forming any pointer
// into its frame, because the probe may move it.
//
// The Go function is a thin wrapper so the trampoline itself stays a few
// instructions long and still appears in coverage, which an assembly-only
// function does not. The compiler inlines it.
func Call(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	if a == nil {
		panic("cppgo: asm.Call: nil Args")
	}
	return call(fn, a)
}

// call is implemented in call_amd64.s: the trampoline itself.
//
//go:noescape
func call(fn unsafe.Pointer, a *Args) (r uintptr, f float64)
