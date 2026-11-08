//go:build amd64

package asm_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
)

// TestCallReentrantCallsThroughTheRegisterBanks pins the reentrant carrier
// against the same fixture the direct trampoline is measured on: the runtime's
// cgo entry must not change which register carries which argument, nor where
// the result comes back.
func TestCallReentrantCallsThroughTheRegisterBanks(t *testing.T) {
	symbol := fixture(t)

	t.Run("integer result", func(t *testing.T) {
		var a asm.Args
		a.I[0] = 40
		a.I[1] = 2
		got, _ := asm.CallReentrant(symbol("cppgo_fixture_add"), &a)
		assert.EqualValues(t, 42, int64(got))
		assert.EqualValues(t, 42, int64(a.Ret), "the wrapper stores RAX in the block")
	})

	t.Run("float result", func(t *testing.T) {
		var a asm.Args
		for i := range 8 {
			a.F[i] = 1
		}
		_, got := asm.CallReentrant(symbol("cppgo_fixture_weighted8"), &a)
		assert.EqualValues(t, 36, got)
		assert.EqualValues(t, 36, a.RetF, "the wrapper stores XMM0 in the block")
	})

	t.Run("mixed arguments", func(t *testing.T) {
		a := &asm.Args{I: [6]uintptr{1, 2}, F: [8]float64{3, 4}}
		_, got := asm.CallReentrant(symbol("cppgo_fixture_mix"), a)
		assert.EqualValues(t, 1+20+300+4000, got)
	})

	t.Run("pointer round trip", func(t *testing.T) {
		buf := []float64{1, 2, 3}
		var a asm.Args
		a.I[0] = uintptr(unsafe.Pointer(&buf[0]))
		got, _ := asm.CallReentrant(symbol("cppgo_fixture_identity_ptr"), &a)
		assert.Equal(t, a.I[0], got)
	})
}

// TestCallReentrantPanicsOnBadInput pins the two programmer errors: a nil
// argument block and a nil function address, both of which would otherwise be
// a call into the void.
func TestCallReentrantPanicsOnBadInput(t *testing.T) {
	symbol := fixture(t)

	assert.PanicsWithValue(t, "cppgo: asm.CallReentrant: nil Args", func() {
		_, _ = asm.CallReentrant(symbol("cppgo_fixture_add"), nil)
	})

	assert.PanicsWithValue(t, "cppgo: asm.CallReentrant: nil fn", func() {
		_, _ = asm.CallReentrant(nil, &asm.Args{})
	})
}

// TestCallReentrantGivesTheCalleeAnAlignedStack is the measurement that found a
// real bug: runtime.cgocall reaches the register-loading wrapper by a tail jump
// rather than an ordinary call, so the stack is 16-aligned at its entry and the
// target must be called without an extra adjustment. A callee compiled with
// SSE stores faults immediately if this is wrong.
func TestCallReentrantGivesTheCalleeAnAlignedStack(t *testing.T) {
	symbol := fixture(t)

	// cppgo_fixture_deep_stack writes one byte per cache line through a
	// 28 KiB volatile frame, and its prologue is compiled with aligned SSE
	// spills.
	var a asm.Args
	a.I[0] = 28 * 1024
	got, _ := asm.CallReentrant(symbol("cppgo_fixture_deep_stack"), &a)

	asm.Probe()
	direct, _ := asm.Call(symbol("cppgo_fixture_deep_stack"), &a)

	require.EqualValues(t, direct, got)
}
