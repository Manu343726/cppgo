package callback_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Manu343726/cppgo/internal/asm"
)

// TestCallReentrantCallsAPlainFunction is the carrier's own smoke test: before
// any callback is involved, a scalar call through runtime.cgocall must behave
// exactly like the direct trampoline.
func TestCallReentrantCallsAPlainFunction(t *testing.T) {
	sym := fixture(t)

	var args asm.Args
	args.I[0] = 40
	args.I[1] = 2
	got, _ := asm.CallReentrant(sym("cb_add"), &args)
	assert.EqualValues(t, 42, int32(got))
}
