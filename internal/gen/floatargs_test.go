package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrampolineFrameAlignsTheResultBlock pins the frame arithmetic the
// assembler agrees with.
//
// The frame is the argument block followed by the result block, and Go's
// assembler starts the result block on the machine's *maximum* alignment, not on
// the result type's: cmd/vendor/.../asmdecl (run by `go vet`) lays the frame out
// with `offset += -offset & (arch.maxAlign - 1)` before the results, and an
// assembly frame it disagrees with does not compile. A float argument leaves the
// argument block 4 bytes short of an 8-byte boundary, which is exactly where an
// align-to-size rule puts the result at 20 and vet expects 24.
//
// The fixture is the shape that found it: a free function whose last argument is
// a float and whose result is one, and a constructor with an SSE last argument
// and no result.
func TestTrampolineFrameAlignsTheResultBlock(t *testing.T) {
	rendered, err := RenderResult(fixtureConfig("floatargs", "floatargs.hpp"))
	require.NoError(t, err)
	asm, goFile := "", ""
	for _, out := range rendered.Outputs {
		switch out.Name {
		case AsmFileName:
			asm = string(out.Data)
		case GenFileName:
			goFile = string(out.Data)
		}
	}

	// func cppgoCall_...(fn uintptr, a0 int64, a1 float32) float32
	//
	//	fn 0..8, a0 8..16, a1 16..20 — the result block starts at 24, so the
	//	frame is 28 bytes and the result lands at ret+24.
	assert.Contains(t, goFile, "func cppgoCall_3c6e719f7c58(fn uintptr, a0 int64, a1 float32) float32")
	assert.Contains(t, asm, "// func cppgoCall_3c6e719f7c58(fn uintptr, a0 int64, a1 float32) float32\nTEXT ·cppgoCall_3c6e719f7c58(SB), NOSPLIT, $0-28")
	assert.Contains(t, asm, "MOVSS\ta1+16(FP), X0")
	assert.Contains(t, asm, "MOVSS\tX0, ret+24(FP)")

	// func cppgoCall_...(fn uintptr, this uintptr, a1 int64, a2 int64, a3 float32)
	//
	//	this 8..16, a1 16..24, a2 24..32, a3 32..36 — no result block at all, so
	//	the frame is exactly the argument block's end, 36.
	assert.Contains(t, asm, "TEXT ·cppgoCall_cbff47a50a07(SB), NOSPLIT, $0-36")
	assert.Contains(t, asm, "MOVSS\ta3+32(FP), X0")

	// A float result with no float argument: the argument block already ends on
	// an 8-byte boundary, and the alignment changes nothing.
	assert.Contains(t, asm, "TEXT ·cppgoCall_e2b7eded1baa(SB), NOSPLIT, $0-20")
	assert.Contains(t, asm, "MOVSS\tX0, ret+16(FP)")
}
