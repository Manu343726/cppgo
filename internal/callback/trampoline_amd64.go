//go:build amd64

package callback

// callbackTableAddr is implemented in trampoline_amd64.s.
func callbackTableAddr() uintptr

// callSlotBytes is the size of one slot's CALL instruction: a five-byte
// relative call. The trampoline divides the return address by it to recover the
// slot, and the layout test below reads the instruction back to prove the
// compiler and this file agree.
const callSlotBytes = 5

// callSlotCount mirrors the number of CALL instructions emitted in
// trampoline_amd64.s; the layout test fails when the two drift apart.
const callSlotCount = 64

// slotAddr returns the address of slot i.
func slotAddr(i int) uintptr {
	return callbackTableAddr() + uintptr(i*callSlotBytes)
}

// slotIndex recovers the slot index of code, reporting false when the address
// is not one of this table's slots.
func slotIndex(code uintptr) (int, bool) {
	base := callbackTableAddr()
	if code < base {
		return 0, false
	}
	offset := code - base
	if offset%callSlotBytes != 0 {
		return 0, false
	}
	index := int(offset / callSlotBytes)
	if index >= callSlotCount {
		return 0, false
	}
	return index, true
}
