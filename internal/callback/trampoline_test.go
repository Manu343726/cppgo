package callback

import (
	"testing"
	"unsafe"
)

// TestSlotIndex covers every way an address can fail to be a slot: outside the
// table, inside it but not on an instruction boundary, and past its last
// instruction.
func TestSlotIndex(t *testing.T) {
	base := callbackTableAddr()

	cases := []struct {
		name  string
		code  uintptr
		index int
		ok    bool
	}{
		{"before the table", base - callSlotBytes, 0, false},
		{"zero", 0, 0, false},
		{"first slot", base, 0, true},
		{"inside a slot", base + 1, 0, false},
		{"last slot", slotAddr(callSlotCount - 1), callSlotCount - 1, true},
		{"one past the table", slotAddr(callSlotCount), 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index, ok := slotIndex(tc.code)
			if ok != tc.ok {
				t.Fatalf("slotIndex(%#x) ok = %v, want %v", tc.code, ok, tc.ok)
			}
			if ok && index != tc.index {
				t.Fatalf("slotIndex(%#x) = %d, want %d", tc.code, index, tc.index)
			}
		})
	}
}

// TestLookupRejectsNonSlots pins the query a binding uses before handing an
// address to C++: an address that is not a live registration of any kind is
// false, and one that is a slot but was released is false too.
func TestLookupRejectsNonSlots(t *testing.T) {
	if Lookup(0) {
		t.Fatal("zero is not a callback")
	}
	if Lookup(callbackTableAddr() + 1) {
		t.Fatal("an address inside an instruction is not a callback")
	}

	code, err := Register(func(*Args) {})
	if err != nil {
		t.Fatal(err)
	}
	if !Lookup(code) {
		t.Fatal("a live registration is a callback")
	}
	if err := Release(code); err != nil {
		t.Fatal(err)
	}
	if Lookup(code) {
		t.Fatal("a released slot is not a callback")
	}
}

// TestTrampolineIsFiveByteRelativeCalls pins the arithmetic the index
// computation depends on: every slot is an E8 relative call, five bytes long,
// so the return address the trampoline sees divided by five names the slot. The
// assembler would silently break this by emitting a longer form.
func TestTrampolineIsFiveByteRelativeCalls(t *testing.T) {
	for i := range callSlotCount {
		b := *(*byte)(ptrAt(slotAddr(i)))
		if b != 0xe8 {
			t.Fatalf("slot %d starts with %#x, want 0xe8 (CALL rel32)", i, b)
		}
	}
}

// ptrAt views a code address as a byte pointer, which is how the layout test
// reads the instruction the assembler emitted. The address is a text address of
// this process, not Go memory.
func ptrAt(addr uintptr) *byte {
	return (*byte)(unsafe.Pointer(addr)) //nolint:govet // a text address, deliberately
}
