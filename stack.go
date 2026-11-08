package cppgo

import "github.com/Manu343726/cppgo/internal/asm"

// StackReserve is the amount of goroutine stack headroom, in bytes, that
// ProbeStack guarantees below the point it is called from.
//
// It covers the deepest C++ frame a bound call can create, which is what makes a
// call safe on a goroutine whose stack has not grown yet.
const StackReserve = asm.StackReserve

// ProbeStack guarantees at least StackReserve bytes of goroutine stack headroom
// below the caller, so a foreign call cannot run out of stack.
//
// Generated wrappers call it before every foreign call, and before any pointer
// into the caller's frame is formed: the call may move that frame, so a pointer
// taken before the probe would be stale.
//
// The headroom belongs to the goroutine and to the depth it was asked at. Called
// from another goroutine, or below the foreign call, it protects nothing. How the
// headroom is obtained is internal to package asm.
func ProbeStack() {
	asm.Probe()
}
