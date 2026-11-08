package callback_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
)

// TestReentrantCarrierStackBudget measures how much stack the C++ frames of a
// callback-capable call have. They run on the M's system stack, not on the
// goroutine's, so cppgo.ProbeStack's 32 KiB reserve does not apply to them; the
// budget is whatever the runtime gives g0. The fixture writes one byte per
// cache line through 28 KiB, which is the same probe the direct carrier passes.
func TestReentrantCarrierStackBudget(t *testing.T) {
	sym := fixture(t)

	var args asm.Args
	args.I[0] = 28 * 1024
	got, _ := asm.CallReentrant(sym("cb_deep_stack"), &args)
	require.NotZero(t, got)

	// The same call through the direct carrier, for the comparison.
	asm.Probe()
	direct, _ := asm.Call(sym("cb_deep_stack"), &args)
	assert.Equal(t, direct, got, "both carriers reach the same fixture result")
}
