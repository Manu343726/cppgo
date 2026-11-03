// Package asm is the architecture-specific heart of cppgo: a NOSPLIT
// trampoline that enters the System V C ABI straight from Go, without the
// runtime's cgo path.
//
// The trampoline does three things and nothing else: it loads the argument
// block into the C argument registers, it calls the foreign function with a
// 16-byte-aligned stack pointer, and it copies the two possible return values
// out of RAX and XMM0.
package asm

// Args is the argument block a trampoline loads before it calls a foreign
// function. Both register banks are filled from the front: I[0..5] become
// RDI, RSI, RDX, RCX, R8, R9 and F[0..7] become XMM0..XMM7.
//
// The layout is load-bearing: the assembly reads I at offsets 0..40 and F at
// offsets 48..104 (Args is 112 bytes, 8-byte aligned, no padding). asm_test.go
// asserts those offsets so a change here fails loudly instead of calling into
// the void.
type Args struct {
	// I holds integer and pointer arguments in declaration order.
	I [6]uintptr
	// F holds floating point arguments in declaration order.
	F [8]float64
}
