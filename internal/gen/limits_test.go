package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTemplateParamIgnoresVolatile pins the identity of a template parameter:
// leading qualifiers are not part of its name.
func TestTemplateParamIgnoresVolatile(t *testing.T) {
	m := &mapper{templateParams: map[string]string{"T": "T"}}
	name, ok := m.templateParam("volatile T")
	assert.True(t, ok, "a volatile-qualified parameter names the same parameter")
	assert.Equal(t, "T", name)

	_, ok = m.templateParam("U")
	assert.False(t, ok, "an unknown name is not a parameter")
}

// TestScanInstantiationsNamesAMissingPackage is the scan's first refusal: a
// pattern that names no Go package says so instead of reporting no
// instantiations.
func TestScanInstantiationsNamesAMissingPackage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module scanprobe\n\ngo 1.27.0\n"), 0o644))
	_, err := scanInstantiations(dir, []string{"./absent"}, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanning ./absent")
}

// TestGenerateRefusesAnUnwritableOutputDirectory proves the generator writes
// nothing it cannot place: an output path under a regular file is an error
// naming the directory, not a partial package.
func TestGenerateRefusesAnUnwritableOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, []byte("not a directory"), 0o644))
	err := Generate(Config{
		Headers: []string{filepath.Join("testdata", "alldefaults", "alldefaults.hpp")},
		Package: "alldefaults",
		Library: "liballdefaults",
		Out:     filepath.Join(file, "out"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating output directory")
}

// TestZeroArgumentDefaultPrefixIsDropped is the contract's rule for a prefix
// with no parameter type to take a suffix from: it is dropped and reported,
// because it would take the name of the full signature.
func TestZeroArgumentDefaultPrefixIsDropped(t *testing.T) {
	rendered, err := RenderResult(fixtureConfig("alldefaults", "alldefaults.hpp"))
	require.NoError(t, err)
	joined := ""
	for _, warning := range rendered.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "the zero-argument spelling is dropped")
	generated := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			generated = string(out.Data)
		}
	}
	assert.Contains(t, generated, "func Sum(a int32, b int32) int32", "the full signature keeps the bare name")
	assert.Contains(t, generated, "func SumInt32(a int32) int32", "a prefix takes the overload suffix")
}
