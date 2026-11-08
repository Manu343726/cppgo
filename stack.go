package cppgo

import "github.com/Manu343726/cppgo/internal/asm"

// StackReserve is the amount of goroutine stack headroom, in bytes, that
// ProbeStack guarantees below the point it is called from.
//
// It has to cover the deepest foreign frame a bound function can create, because
// the trampoline switches the stack pointer and calls straight into C, where the
// Go runtime cannot grow the stack for it. It must match the frame size of the
// assembly probe in internal/asm; the deep-stack test fails on a mismatch.
const StackReserve = asm.StackReserve

// ProbeStack guarantees at least StackReserve bytes of goroutine stack headroom
// below the caller, so a foreign call cannot grow past stack.lo.
//
// Generated wrappers call it before every foreign call, and before any pointer
// into the caller's frame is formed: the probe may move the stack, so a pointer
// taken before it would be stale. The first call on a goroutine grows the stack
// through the runtime's own growth path, and every later call is a frame
// adjustment.
//
// The mechanism lives in internal/asm: entering an assembly function with a
// 32 KiB frame makes the runtime grow the stack to fit it, and the grown stack
// stays grown after the return. A Go function with the same local array would
// cost more, because the compiler zeroes a large local on entry (~1.2 µs per
// call, measured), and the probe has to cost the same order as the call it
// protects.
//
// The headroom is per goroutine and per depth. Called from another goroutine, or
// below the foreign call, it protects nothing.
func ProbeStack() {
	asm.Probe()
}
