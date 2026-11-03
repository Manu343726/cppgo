//go:build !amd64

package asm

import "sync/atomic"

// probeSink keeps the probe's frame observable.
var probeSink atomic.Uint32

// Probe grows the goroutine's stack so that at least StackReserve bytes are
// allocated below the caller.
//
// This is the fallback for architectures cppgo cannot call into: with no
// assembly trampoline there is nothing to protect, but ProbeStack still has to
// be a well-behaved function rather than a panic, so it grows the stack the
// portable way — with a large local array that the compiler cannot drop. It
// costs a 32 KiB memclr per call, which is why the amd64 build does the same
// job with an assembly frame.
//
//go:noinline
func Probe() {
	var frame [StackReserve]byte

	frame[0] = 1
	frame[StackReserve-1] = frame[0] + 1

	probeSink.Store(uint32(frame[StackReserve-1]))
}
