package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallbackPayloads covers the shapes a callback parameter may carry beyond
// a scalar: the callee's text as a copied Go string (NUL-terminated or counted),
// its bytes as a slice view, and a record pointer as a mirrored Go pointer. The
// C++ side owns every one of these bytes, which is the rule that lets them be
// read while the callback runs and never afterwards.
func TestCallbackPayloads(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "payloads", "payloads.hpp")},
		Package: "payloads",
		Library: "libpayloads",
		Out:     ".",
	})
	require.NoError(t, err)
	for _, warning := range rendered.Warnings {
		assert.NotContains(t, warning, "not bound", "every payload shape binds")
	}

	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func SetLogCallback(fn func(int32, string)) error",
		"a NUL-terminated const char * is a Go string")
	assert.Contains(t, goFile, "cppgoGoString(cppgoWord(cppgoFrame.I[1]))",
		"the string is copied out of the callee's bytes during the callback")
	assert.Contains(t, goFile, "func SetText(fn func(string)) error",
		"a counted text buffer is one Go string, not two arguments")
	assert.Contains(t, goFile, "cppgoGoBytes(cppgoWord(cppgoFrame.I[0]), cppgoFrame.I[1])",
		"the count says how many bytes to read")
	assert.Contains(t, goFile, "func SetBytes(fn func([]uint8)) error",
		"a counted byte buffer is a slice view of the callee's memory")
	assert.Contains(t, goFile, "cppgoGoSlice[uint8](cppgoWord(cppgoFrame.I[0]), cppgoFrame.I[1])")
	assert.Contains(t, goFile, "func SetPoints(fn func([]Point)) error",
		"a counted record buffer is a slice of the mirrored struct")
	assert.Contains(t, goFile, "func SetPoint(fn func(*Point)) error",
		"a record pointer is the mirrored struct's address")
	assert.Contains(t, goFile, "func SetUntyped(fn func(unsafe.Pointer)) error",
		"a void pointer stays untyped")
	assert.Contains(t, goFile, "func SetLevel(fn func(int32, string)) (int32, error)",
		"a payload does not cost the callback its result")

	shimSource := outputNamed(t, rendered, ShimSourceFileName)
	assert.Contains(t, shimSource, "void (*)(int32_t, const char *)",
		"the shim casts the address to the declaration's own signature")
	assert.Contains(t, shimSource, "void (*)(const uint8_t *, uint64_t)",
		"a counted pair is two C parameters, and the cast keeps both")
}
