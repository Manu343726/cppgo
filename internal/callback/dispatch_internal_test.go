package callback

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDispatchSymbolIsReferenced pins the one reference the trampoline cannot
// make in Go: trampoline_amd64.s loads ·dispatch's address, and nothing in Go
// calls the function it points at, so this test is what keeps the dispatcher —
// and the frame type it is written against — from reading as dead code.
func TestDispatchSymbolIsReferenced(t *testing.T) {
	assert.NotNil(t, dispatch, "the address the assembly loads")
	var frame *callFrame
	assert.Nil(t, frame, "the frame the trampoline fills")
}
