package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallbackAliasIsTheFunctionPointer proves `using CountCallback = void
// (*)(int)` binds as the function pointer it names: the declaration is
// recognised as a callback, and the Go function type comes from the pointee's
// own signature rather than from the alias's spelling.
func TestCallbackAliasIsTheFunctionPointer(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "aliases", "aliases.hpp")},
		Package: "aliases",
		Library: "libaliases",
		Out:     ".",
	})
	require.NoError(t, err)
	for _, warning := range rendered.Warnings {
		assert.NotContains(t, warning, "not bound", "an aliased callback binds with no refusal")
	}

	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func SetCountCallback(fn func(int32)) error",
		"the alias is the function pointer, and the pointer's signature is the Go function type")
	assert.NotContains(t, goFile, "LogCallback", "the Go API names the signature, not the C++ alias")
}
