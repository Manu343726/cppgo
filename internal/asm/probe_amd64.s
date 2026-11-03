#include "textflag.h"

// func probe()
//
// A frame of StackReserve (32 KiB) bytes with nothing in it. Two properties
// come from that frame, and both are the point of the function:
//
//  1. The assembler's stack-split preamble, which every non-NOSPLIT assembly
//     function gets, subtracts the frame size when it compares SP against the
//     stack guard, and calls runtime.morestack when the result would not fit.
//     Entering this function is therefore how a goroutine's stack is grown
//     before a foreign call.
//
//  2. When the function returns, the frame is still allocated below the
//     caller's SP: that is the headroom the foreign call runs in.
//
// The store touches the byte at the bottom of the frame, which is the byte a
// foreign call reaches first. It is a single byte, not a page-per-page loop:
// the memory is already mapped (Go stacks are), so what matters is that the
// frame is real, and a store to the bottom of it is what makes that true.
//
// The frame must NOT be zeroed, which is exactly why this is assembly: see the
// note in probe_amd64.go.
TEXT ·probe(SB), $32768-0
	MOVB	AX, 0(SP)
	RET
