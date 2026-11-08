package cppgo

import "github.com/Manu343726/cppgo/internal/asm"

// StackReserve is the amount of goroutine stack headroom, in bytes, that
// ProbeStack guarantees below the point it is called from.
//
// It has to cover the deepest foreign frame a bound function can create,
// because the trampoline switches the stack pointer and calls straight into C,
// where the Go runtime cannot grow the stack for it.
const StackReserve = asm.StackReserve

// ProbeStack guarantees at least StackReserve bytes of goroutine stack
// headroom below the caller. A foreign call cannot then grow past stack.lo.
//
// Generated wrappers call it before every foreign call. It is cheap. The first
// call on a goroutine grows its stack through the runtime's normal growth path
// (entering a function whose frame exceeds the stack), and every later call is
// a frame adjustment.
//
// The mechanism lives in internal/asm. Raising the stack pointer's distance
// from stack.lo is done by entering an assembly function with a 32 KiB frame.
// A Go function with a 32 KiB local array would cost more, because the compiler
// zeroes a large local array on entry (~1.2 µs per call, measured), and the
// probe has to cost the same order as the call it protects.
//
// ProbeStack is a no-op on its own. It has an effect only when it is called
// from the same goroutine, at a stack depth at or above the foreign call.
func ProbeStack() {
	asm.Probe()
}
