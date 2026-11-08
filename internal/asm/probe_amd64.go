//go:build amd64

package asm

// Probe grows the goroutine's stack so that at least StackReserve bytes are
// allocated below the caller, and returns immediately.
//
// The growth is a side effect of entering a non-NOSPLIT assembly function with
// a StackReserve-byte frame. The assembler's preamble compares the stack
// pointer against the stack guard *including the frame size* and calls
// runtime.morestack when the frame exceeds the available stack. That is the
// runtime's own growth path. The frame is left allocated below the caller when
// the function returns, and that frame is the headroom a foreign call gets to
// use.
//
// The frame lives in assembly. A Go function with a large local array would
// cost more, because the Go compiler zeroes a large local that may be read (a
// REP STOSQ over 32 KiB on entry, ~1.2 µs per call on this machine), and that
// would make the probe 80x more expensive than the call it protects. Assembly
// has no such initialisation.
func Probe() { probe() }

// probe is implemented in probe_amd64.s: a StackReserve-byte frame with one
// store in it.
func probe()
