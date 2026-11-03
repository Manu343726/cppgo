package asm

// StackReserve is the goroutine stack headroom, in bytes, that Probe
// guarantees below the point it is called from: enough for the deepest
// generated C++ shim frame, and small enough that the probe is a frame
// adjustment rather than a copy.
//
// It must match the frame size of probe in probe_amd64.s; the deep-stack test
// in the cppgo package fails if it does not.
const StackReserve = 32 << 10
