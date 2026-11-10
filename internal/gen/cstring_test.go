package gen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCStringParameterAndResult covers the two directions of a `const char *`:
// a parameter is a NUL-terminated copy the caller keeps alive (including on a
// constructor, whose declarations used to be emitted without their stdDecls),
// and a result is copied into a Go string whether it arrives in the result
// register or through an out-pointer.
func TestCStringParameterAndResult(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "cstring", "cstring.hpp")},
		Package: "cs",
		Library: "libcs",
		Out:     ".",
	})
	require.NoError(t, err)
	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "cppgoCString(name)", "a char * parameter is a NUL-terminated copy")
	assert.Contains(t, goFile, "cppgo.KeepAlive(", "the copy is kept alive across the call")
	assert.Contains(t, goFile, "func (n *Named) Name() (string, error)", "a char * result is a Go string")
	assert.Contains(t, goFile, "cppgoGoString(", "the result is copied out of the library's pointer")
	assert.True(t, strings.Contains(goFile, "func NewNamed("), "the constructor with a char * parameter binds")
}
