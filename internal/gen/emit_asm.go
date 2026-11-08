package gen

import (
	"fmt"
	"strings"
)

// emitAsm renders zz_cppgo_amd64.s: one NOSPLIT trampoline per binding.
//
// Each trampoline reads its arguments out of the Go frame first, saves SP in
// R13, aligns it to 16 bytes (SysV wants RSP 16-byte aligned before the CALL),
// calls through R12 and restores SP before storing the result, because the
// result slot is addressed relative to SP. R12 and R13 are callee-saved in
// SysV, so the C function preserves them.
func (m *packageModel) emitAsm() []byte {
	var b strings.Builder
	b.WriteString(GeneratedBanner + "\n\n")
	b.WriteString("#include \"textflag.h\"\n")
	for _, binding := range m.bindings {
		b.WriteString("\n")
		fmt.Fprintf(&b, "// %s\n", trampolineSignature(binding))
		fmt.Fprintf(&b, "TEXT ·%s(SB), NOSPLIT, $0-%d\n", binding.Trampoline, binding.Frame)
		b.WriteString("\tMOVQ\tfn+0(FP), R12\n")
		for _, slot := range binding.Slots[1:] {
			if slot.label == "ret" {
				continue
			}
			fmt.Fprintf(&b, "\t%s\t%s+%d(FP), %s\n", loadInstruction(slot), slot.label, slot.offset, slot.reg)
		}
		b.WriteString("\tMOVQ\tSP, R13\n")
		b.WriteString("\tANDQ\t$~15, SP\n")
		b.WriteString("\tCALL\tR12\n")
		b.WriteString("\tMOVQ\tR13, SP\n")
		if ret := resultSlotOf(binding); ret != nil {
			fmt.Fprintf(&b, "\t%s\t%s, %s+%d(FP)\n", storeInstruction(ret), resultRegister(ret), ret.label, ret.offset)
		}
		b.WriteString("\tRET\n")
	}
	if m.usesFree {
		b.WriteString("\n// func cppgoFree(fn uintptr, block unsafe.Pointer)\n")
		b.WriteString("TEXT ·cppgoFree(SB), NOSPLIT, $0-16\n")
		b.WriteString("\tMOVQ\tfn+0(FP), R12\n")
		b.WriteString("\tMOVQ\tblock+8(FP), DI\n")
		b.WriteString("\tMOVQ\tSP, R13\n")
		b.WriteString("\tANDQ\t$~15, SP\n")
		b.WriteString("\tCALL\tR12\n")
		b.WriteString("\tMOVQ\tR13, SP\n")
		b.WriteString("\tRET\n")
	}
	return []byte(b.String())
}

// resultSlotOf returns the result word the trampoline stores, or nil.
func resultSlotOf(binding *binding) *slot {
	for _, slot := range binding.Slots {
		if slot.label == "ret" {
			return slot
		}
	}
	return nil
}

// loadInstruction picks the move that reads one argument word.
func loadInstruction(slot *slot) string {
	if slot.class == classFloat {
		if slot.size == 4 {
			return "MOVSS"
		}
		return "MOVSD"
	}
	// An integer argument is always passed in a full 64-bit register; a
	// 4-byte integer slot is only a status result, which is stored, not loaded.
	return "MOVQ"
}

// storeInstruction picks the move that writes the result word.
func storeInstruction(slot *slot) string {
	if slot.class == classFloat {
		if slot.size == 4 {
			return "MOVSS"
		}
		return "MOVSD"
	}
	if slot.size == 4 {
		return "MOVL"
	}
	return "MOVQ"
}

// resultRegister is the register the C ABI returns a value in.
func resultRegister(slot *slot) string {
	if slot.class == classFloat {
		return "X0"
	}
	return "AX"
}

// trampolineSignature is the Go signature of a trampoline, as a comment.
func trampolineSignature(binding *binding) string {
	parts := make([]string, 0, len(binding.Slots))
	result := ""
	for _, slot := range binding.Slots {
		if slot.label == "ret" {
			result = " " + slot.goType
			continue
		}
		parts = append(parts, slot.label+" "+slot.goType)
	}
	return "func " + binding.Trampoline + "(" + strings.Join(parts, ", ") + ")" + result
}
