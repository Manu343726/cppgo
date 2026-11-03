//go:build amd64

package asm

import "unsafe"

// Call invokes the foreign function at fn with the arguments in a.
//
// The integer result comes back from RAX and the floating point result from
// XMM0; a caller uses whichever one its signature says. When a signature has
// both (it should not), the integer result is the RAX value.
//
// a must not be nil and fn must not be nil. Call does not grow the Go stack,
// so the caller owns the stack guarantee: call cppgo.ProbeStack first if the
// foreign function may use more than a few hundred bytes of stack.
//
// Call is a thin wrapper around the assembly trampoline so that the trampoline
// itself stays a few instructions long while remaining covered by tests and by
// the coverage gate (an assembly-only function carries no Go coverage
// counters). The compiler inlines it.
func Call(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	if a == nil {
		panic("cppgo: asm.Call: nil Args")
	}
	return call(fn, a)
}

// call is implemented in call_amd64.s.
//
//go:noescape
func call(fn unsafe.Pointer, a *Args) (r uintptr, f float64)
