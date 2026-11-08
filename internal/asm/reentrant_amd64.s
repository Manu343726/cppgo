//go:build amd64

#include "textflag.h"

// reentrantWrapper is the function runtime.cgocall calls (declared in
// reentrant_wrapper_amd64.go). cgocall passes its second argument in DI with
// the C calling convention; the block it points at is
//
//	0(DI)  target function address
//	8(DI)  *Args (I[0..5] at 0..40, F[0..7] at 48..104, results at 112..120)
//
// The wrapper loads both register banks, calls the target, and stores RAX and
// XMM0 into the result slots of the same block. It is the argument half of the
// System V ABI that cgo's one-argument call cannot express.
//
// R12 (the target) and R13 (the argument block) hold state across the call and
// both are callee-saved by SysV, so the callee preserves them; R14 holds g and
// is deliberately never touched.
TEXT ·reentrantWrapper(SB), NOSPLIT, $0-0
	MOVQ 0(DI), R12
	MOVQ 8(DI), R13
	MOVQ R13, AX

	MOVQ 0(AX), DI
	MOVQ 8(AX), SI
	MOVQ 16(AX), DX
	MOVQ 24(AX), CX
	MOVQ 32(AX), R8
	MOVQ 40(AX), R9

	MOVSD 48(AX), X0
	MOVSD 56(AX), X1
	MOVSD 64(AX), X2
	MOVSD 72(AX), X3
	MOVSD 80(AX), X4
	MOVSD 88(AX), X5
	MOVSD 96(AX), X6
	MOVSD 104(AX), X7

	// cgocall reaches this function by a tail jump from its landing pad, so
	// RSP is 16-aligned here (not 8 mod 16 as after an ordinary CALL), and
	// the target must see RSP+8 16-aligned. Calling with RSP as it is puts
	// the target exactly there; measured with a fixture that reports its own
	// RSP, because getting this wrong is a SIGSEGV inside an aligned SSE
	// store in the callee, not a wrong answer.
	CALL R12

	// The results, once the argument registers are free to be reused.
	MOVQ AX, 112(R13)
	MOVSD X0, 120(R13)
	XORQ AX, AX
	RET

// reentrantWrapperPtr returns the ABI0 address of reentrantWrapper, which is
// the form runtime.cgocall expects.
TEXT ·reentrantWrapperPtr(SB), NOSPLIT, $0-8
	LEAQ ·reentrantWrapper(SB), AX
	MOVQ AX, ret+0(FP)
	RET
