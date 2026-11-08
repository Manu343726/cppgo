//go:build stackalloc

#include "textflag.h"

// func callOut(fn uintptr, this uintptr, out unsafe.Pointer) int32
//
// The generated accessor's shape, reproduced: a NOSPLIT trampoline with no
// //go:noescape on its declaration (so the compiler must assume `out` escapes)
// that enters the C ABI with a 16-byte-aligned stack and copies RAX back.
//
// It exists so the benchmarks can price the emitted accessor, including the
// allocation its out-pointer costs, against the value form.
//
//	fn+0(FP)   foreign function address
//	this+8(FP) the object address (DI)
//	out+16(FP) the result pointer (SI)
//	ret+24(FP) the status (AX)
TEXT ·callOut(SB), NOSPLIT, $0-28
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVQ	out+16(FP), SI

	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP

	MOVL	AX, ret+24(FP)
	RET
