package asm

// StackReserve is the goroutine stack headroom, in bytes, that Probe
// guarantees below the point it is called from. That is enough for the deepest
// generated C++ shim frame, and small enough that the probe costs a frame
// adjustment.
//
// It must match the frame size of probe in probe_amd64.s. The deep-stack test
// in the cppgo package fails on a mismatch.
const StackReserve = 32 << 10
