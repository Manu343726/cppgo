#include "textflag.h"

// func call(fn unsafe.Pointer, a *Args) (uintptr, float64)
//
// Go-side frame (ARG+RET, 32 bytes):
//
//	fn+0(FP)   foreign function address
//	a+8(FP)    *Args argument block
//	r+16(FP)   integer result  (RAX)
//	f+24(FP)   float result    (XMM0)
//
// Args layout (see args.go, asserted in asm_test.go):
//
//	I[0..5] at 0, 8, 16, 24, 32, 40  -> RDI, RSI, RDX, RCX, R8, R9
//	F[0..7] at 48, 56, ..., 104      -> XMM0..XMM7
//
// Two rules make this correct, both of them about FP being SP-relative:
//
//  1. every argument is read BEFORE SP is switched, and
//  2. every result is stored AFTER SP is restored.
//
// The call needs RSP 16-byte aligned immediately before CALL (SysV: the callee
// expects RSP+8 to be 16-byte aligned at its entry point), and $0-32 leaves the
// frame untouched, so the only registers used to carry state across the call
// are R12 (the target) and R13 (the saved SP), both callee-saved by SysV.
//
// R14 holds g in the Go ABI and is deliberately never touched here.
TEXT ·call(SB), NOSPLIT, $0-32
	MOVQ fn+0(FP), R12
	MOVQ a+8(FP), AX

	// Integer/pointer bank.
	MOVQ 0(AX), DI
	MOVQ 8(AX), SI
	MOVQ 16(AX), DX
	MOVQ 24(AX), CX
	MOVQ 32(AX), R8
	MOVQ 40(AX), R9

	// Floating point bank.
	MOVSD 48(AX), X0
	MOVSD 56(AX), X1
	MOVSD 64(AX), X2
	MOVSD 72(AX), X3
	MOVSD 80(AX), X4
	MOVSD 88(AX), X5
	MOVSD 96(AX), X6
	MOVSD 104(AX), X7

	// Enter the C ABI.
	MOVQ SP, R13
	ANDQ $~15, SP
	CALL R12
	MOVQ R13, SP

	// Results, once FP is meaningful again.
	MOVQ AX, r+16(FP)
	MOVSD X0, f+24(FP)
	RET
