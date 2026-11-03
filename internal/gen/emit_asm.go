package gen

import (
	"fmt"
	"strings"
)

// emitAsm renders zz_cppgo_amd64.s.
//
// Every trampoline is a NOSPLIT function with no frame. It reads the C
// arguments out of the Go stack frame first, then saves SP in R13, aligns it
// to 16 bytes (SysV wants RSP 16-byte aligned *before* the CALL), calls
// through R12 and restores SP before storing the result, because the result
// slot is addressed relative to SP.
//
// R12 and R13 are callee-saved in SysV, so the C function preserves them.
func emitAsm(sigs []*signature) []byte {
	var b strings.Builder
	b.WriteString(GeneratedBanner + "\n\n")
	b.WriteString("#include \"textflag.h\"\n")
	for _, sig := range sigs {
		b.WriteString("\n")
		fmt.Fprintf(&b, "// %s\n", trampolineSignature(sig))
		fmt.Fprintf(&b, "TEXT ·%s(SB), NOSPLIT, $0-%d\n", sig.fn.trampName(), sig.total)
		b.WriteString("\tMOVQ\tfn+0(FP), R12\n")
		for _, sl := range sig.slots[1:] {
			instr := "MOVQ"
			if sl.class == classFloat {
				instr = "MOVSS"
				if sl.size == 8 {
					instr = "MOVSD"
				}
			}
			fmt.Fprintf(&b, "\t%s\t%s+%d(FP), %s\n", instr, sl.label, sl.offset, sl.reg)
		}
		b.WriteString("\tMOVQ\tSP, R13\n")
		b.WriteString("\tANDQ\t$~15, SP\n")
		b.WriteString("\tCALL\tR12\n")
		b.WriteString("\tMOVQ\tR13, SP\n")
		if sl := sig.ret.slot; sl != nil {
			instr, reg := storeInstruction(sl)
			fmt.Fprintf(&b, "\t%s\t%s, %s+%d(FP)\n", instr, reg, sl.label, sl.offset)
		}
		b.WriteString("\tRET\n")
	}
	return []byte(b.String())
}

// storeInstruction picks the instruction and source register for the result.
func storeInstruction(sl *slot) (instr, reg string) {
	switch sl.goType {
	case "int32":
		return "MOVL", "AX"
	case "float32":
		return "MOVSS", "X0"
	case "float64":
		return "MOVSD", "X0"
	}
	return "MOVQ", "AX"
}

// trampolineSignature is the Go signature of a trampoline, as a comment.
func trampolineSignature(sig *signature) string {
	result := ""
	if sl := sig.ret.slot; sl != nil {
		result = " (ret " + sl.goType + ")"
	}
	parts := make([]string, 0, len(sig.slots))
	for _, sl := range sig.slots {
		parts = append(parts, sl.label+" "+sl.goType)
	}
	return "func " + sig.fn.trampName() + "(" + strings.Join(parts, ", ") + ")" + result
}
