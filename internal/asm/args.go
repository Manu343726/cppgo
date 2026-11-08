// Package asm is the architecture-specific half of the cppgo runtime: the two
// call carriers, whose trampolines enter the System V AMD64 C ABI straight from
// Go without the runtime's cgo path, and the stack probe.
//
// Call loads an argument block into the C argument registers, calls the foreign
// address with a 16-byte-aligned stack pointer, and copies RAX and XMM0 back,
// from a NOSPLIT assembly function with no safepoints. CallReentrant reaches the
// same ABI through runtime.cgocall, which is the only state in which the callee
// may call back into Go (see package callback).
//
// Only amd64 is implemented. The !amd64 files keep this package compiling and
// panic on the first foreign call.
package asm

// Args is the argument block a trampoline loads before it calls a foreign
// function, and the block a callback reads its arguments from.
//
// Both register banks are filled from the front: I[0..5] become RDI, RSI, RDX,
// RCX, R8, R9, and F[0..7] become XMM0..XMM7; Ret and RetF carry RAX and XMM0
// back. The layout is part of the ABI contract. The assembly reads I at offsets
// 0..40, F at 48..104 and the result slots at 112..120, and Args is 128 bytes
// with no padding. asm_test.go asserts those offsets, so a change here fails the
// test instead of corrupting a call. Call and the callback trampoline share the
// block, and a result slot is written only by the carrier that owns the
// direction.
type Args struct {
	// I holds integer and pointer arguments in declaration order.
	I [6]uintptr
	// F holds floating point arguments in declaration order.
	F [8]float64
	// Ret is the INTEGER result slot (RAX).
	Ret uintptr
	// RetF is the SSE result slot (XMM0).
	RetF float64
}
