package cppgo

import "github.com/Manu343726/cppgo/internal/asm"

// StackReserve is the amount of goroutine stack headroom, in bytes, that
// ProbeStack guarantees below the point it is called from.
//
// It has to cover the deepest foreign frame a bound function can create: the
// trampoline switches the stack pointer and calls straight into C, where the
// Go runtime cannot grow the stack for it.
const StackReserve = asm.StackReserve

// ProbeStack guarantees at least StackReserve bytes of goroutine stack
// headroom below the caller, so a foreign call cannot grow past stack.lo.
//
// Generated wrappers call it before every foreign call. It is cheap: the first
// call on a goroutine grows its stack through the runtime's normal growth path
// (entering a function whose frame does not fit), and every later call is a
// frame adjustment.
//
// The mechanism lives in internal/asm: raising the stack pointer's distance
// from stack.lo is done by entering an assembly function with a 32 KiB frame,
// not by a Go function with a 32 KiB local array, because the compiler zeroes
// a large local array on entry (~1.2 µs per call, measured) and the probe has
// to cost the same order as the call it protects.
//
// ProbeStack is a no-op on its own: it protects nothing unless it is called
// from the same goroutine, at a stack depth at or above the foreign call.
func ProbeStack() {
	asm.Probe()
}
