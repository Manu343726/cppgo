// Package asm is the architecture-specific heart of cppgo. It holds a NOSPLIT
// trampoline that enters the System V C ABI straight from Go, without the
// runtime's cgo path.
//
// The trampoline does three things. It loads the argument block into the C
// argument registers, it calls the foreign function with a 16-byte-aligned
// stack pointer, and it copies the two possible return values out of RAX and
// XMM0.
package asm

// Args is the argument block a trampoline loads before it calls a foreign
// function, and the block a callback reads its arguments from. Both register
// banks are filled from the front. I[0..5] become RDI, RSI, RDX, RCX, R8, R9,
// and F[0..7] become XMM0..XMM7. Ret and RetF carry RAX and XMM0 back.
//
// The layout is load-bearing. The assembly reads I at offsets 0..40, F at
// 48..104 and the two result slots at 112..120 (Args is 128 bytes, 8-byte
// aligned, no padding). asm_test.go asserts those offsets. A mismatch fails the
// test before it can corrupt a call. Call and the callback trampoline use
// the same block. A result slot is written only by the carrier that owns the
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
