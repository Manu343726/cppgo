//go:build amd64

#include "textflag.h"

// callbackTable is the address space C calls into. Each slot is one five-byte
// relative CALL to callbackasm1, so slot i's address is callbackTable + 5*i and
// callbackasm1 recovers i by dividing the return address by the instruction
// length. Nothing here returns in the usual sense: callbackasm1 leaves the
// C-ABI frame untouched and returns straight to the C caller, so a slot that
// returns means the table was entered at a bad address, which the Go
// dispatcher reports as a released slot.
TEXT ·callbackTable(SB), NOSPLIT|NOFRAME, $0
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	RET

// callbackasm1 is entered by C through one slot of callbackTable, with the C
// calling convention intact: the argument registers hold the callback's
// arguments and 0(SP) is the return address into C.
//
// The frame it builds below SP is
//
//	  0..127(SP)   Args: I at 0..40, F at 48..104, Ret at 112, RetF at 120
//	128(SP)       callFrame.index
//	136(SP)       callFrame.args
//	144(SP)       the C caller's SP, kept across the Go call
//
// and then it calls crosscall2, which saves the host's callee-saved registers
// and enters runtime.cgocallback. That is the only runtime entry by which C may
// enter Go: it switches to the owning goroutine's stack and runs the callback
// under it. The results come back out of the Args block and into RAX and XMM0,
// and the C caller's return address is put back so RET returns to C.
//
// Stack-passed arguments are not read (a callback takes at most six INTEGER and
// eight SSE arguments); 200 bytes is a multiple of 16 plus 8 so that RSP is
// 16-aligned immediately before the call, as SysV requires.
TEXT ·callbackasm1(SB), NOSPLIT|NOFRAME, $0
	MOVQ 0(SP), R11      // return address, inside the slot table
	MOVQ 8(SP), R10      // the C caller's return address
	ADDQ $8, SP          // drop this slot's return address: SP is now the C caller's

	SUBQ $200, SP
	MOVQ DI, 0(SP)
	MOVQ SI, 8(SP)
	MOVQ DX, 16(SP)
	MOVQ CX, 24(SP)
	MOVQ R8, 32(SP)
	MOVQ R9, 40(SP)
	MOVSD X0, 48(SP)
	MOVSD X1, 56(SP)
	MOVSD X2, 64(SP)
	MOVSD X3, 72(SP)
	MOVSD X4, 80(SP)
	MOVSD X5, 88(SP)
	MOVSD X6, 96(SP)
	MOVSD X7, 104(SP)

	// slot = (return address - callbackTable) / 5 - 1
	MOVQ R11, AX
	LEAQ ·callbackTable(SB), CX
	SUBQ CX, AX
	XORQ DX, DX
	MOVQ $5, CX
	DIVQ CX
	DECQ AX

	MOVQ AX, 128(SP)     // callFrame.index
	LEAQ 0(SP), AX
	MOVQ AX, 136(SP)     // callFrame.args
	MOVQ R10, 144(SP)    // the C caller's SP

	MOVQ ·dispatch(SB), DI
	MOVQ (DI), DI
	LEAQ 128(SP), SI
	MOVQ $0, CX
	CALL crosscall2(SB)

	MOVQ 112(SP), AX
	MOVSD 120(SP), X0
	MOVQ 144(SP), R10
	ADDQ $200, SP
	MOVQ R10, 0(SP)      // restore the C return address where RET will find it
	RET

// callbackTableAddr returns the address of slot 0.
TEXT ·callbackTableAddr(SB), NOSPLIT, $0-8
	LEAQ ·callbackTable(SB), AX
	MOVQ AX, ret+0(FP)
	RET

TEXT ·dispatchWords(SB), NOSPLIT, $0-16
	MOVQ ·dispatch(SB), AX
	MOVQ AX, funval+0(FP)
	MOVQ (AX), BX
	MOVQ BX, code+8(FP)
	RET
