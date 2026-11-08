//go:build amd64

package asm

// Probe grows the goroutine's stack so that at least StackReserve bytes are
// allocated below the caller, and returns immediately.
//
// The growth is a side effect of entering a non-NOSPLIT assembly function with a
// StackReserve-byte frame. The assembler's preamble compares the stack pointer
// against the stack guard including the frame size, and calls runtime.morestack
// when the frame does not fit, which is the runtime's own growth path. The frame
// is still allocated below the caller's SP when the function returns, and that
// is the headroom the foreign call runs in.
//
// The frame lives in assembly because a Go function with a large local array
// pays for zeroing it: a REP STOSQ over 32 KiB on entry, ~1.2 µs per call on
// this machine, which would make the probe 80x more expensive than the call it
// protects. Assembly has no such initialization.
func Probe() { probe() }

// probe is implemented in probe_amd64.s: a StackReserve-byte frame with one
// store in it.
func probe()
