package cxx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSonameFallsBackToTheBaseName covers the common build: a shared object
// built without -Wl,-soname records no DT_SONAME, and the name the linker was
// given is the one a DT_NEEDED entry would take.
func TestSonameFallsBackToTheBaseName(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	library := buildFixtureLibrary(t, dir, compiler)

	name, err := Soname(library)
	require.NoError(t, err)
	assert.Equal(t, "libmathx.so", name)
}

// TestSonameReadsTheDeclaredName is the measured half: a library whose build
// declares a soname is recorded under that name, whatever its file is called.
func TestSonameReadsTheDeclaredName(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	library := filepath.Join(dir, "libmathx.so")

	output, err := exec.Command(compiler, "-std=c++17", "-O1", "-fPIC", "-shared", "-I"+dir,
		"-Wl,-soname,libmathx.so.4", "-o", library, filepath.Join(dir, "mathx.cpp")).CombinedOutput()
	require.NoErrorf(t, err, "building the fixture library: %s", output)

	name, err := Soname(library)
	require.NoError(t, err)
	assert.Equal(t, "libmathx.so.4", name)
}

// TestSonameErrors covers the two inputs that are not a shared object.
func TestSonameErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := Soname(filepath.Join(dir, "absent.so"))
	assert.Error(t, err)

	notELF := filepath.Join(dir, "not-elf.so")
	require.NoError(t, os.WriteFile(notELF, []byte("not an ELF file"), 0o644))
	_, err = Soname(notELF)
	assert.Error(t, err)
}
