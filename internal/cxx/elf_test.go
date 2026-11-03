package cxx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinedDynamicSymbols(t *testing.T) {
	library, err := Load()
	if err != nil {
		t.Skipf("libclang is not installed: %v", err)
	}
	defer func() { _ = library.Close() }()

	// libclang itself is a shared object we know exports its own API.
	names, err := DefinedDynamicSymbols(library.Path())
	require.NoError(t, err)
	require.NotEmpty(t, names)
	assert.Contains(t, names, "clang_getClangVersion")
	assert.Contains(t, names, "clang_visitChildren")
	// Undefined symbols are imports and must not be listed.
	assert.NotContains(t, names, "")
	assert.True(t, isSorted(names), "the result must be sorted for stable comparisons")
}

func TestDefinedDynamicSymbolsAgreesWithNm(t *testing.T) {
	library, err := Load()
	if err != nil {
		t.Skipf("libclang is not installed: %v", err)
	}
	defer func() { _ = library.Close() }()
	if _, err := exec.LookPath("nm"); err != nil {
		t.Skip("nm is not installed")
	}

	fromELF, err := DefinedDynamicSymbols(library.Path())
	require.NoError(t, err)
	fromNm := nmDefinedSymbols(t, library.Path())
	assert.Equal(t, fromNm, fromELF, "the ELF reader must see exactly what nm -D --defined-only sees")
}

func TestDefinedDynamicSymbolsOnFixtureLibrary(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	library := buildFixtureLibrary(t, dir, compiler)

	names, err := DefinedDynamicSymbols(library)
	require.NoError(t, err)
	// The complete-object constructor and the const method defined out of line.
	assert.Contains(t, names, "_ZN5mathx6VectorC1Edd")
	assert.Contains(t, names, "_ZNK5mathx6Vector4normEv")
	// The inline accessor: no symbol at all, which is what the generator has to
	// know before it emits a trampoline.
	assert.NotContains(t, names, "_ZNK5mathx6Vector1xEv")
	// An object file's undefined imports (the C++ runtime) are filtered out.
	assert.NotContains(t, names, "sqrt")
	assert.NotContains(t, names, "_Unwind_Resume")
}

func TestDefinedDynamicSymbolsErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := DefinedDynamicSymbols(filepath.Join(dir, "absent.so"))
	assert.Error(t, err)

	notELF := filepath.Join(dir, "not-elf.so")
	require.NoError(t, os.WriteFile(notELF, []byte("this is not an ELF file"), 0o644))
	_, err = DefinedDynamicSymbols(notELF)
	assert.Error(t, err)

	// A relocatable object has no dynamic symbol table.
	compiler := cxxCompiler(t)
	source := filepath.Join(dir, "mathx.cpp")
	for _, name := range []string{"mathx.cpp", "mathx.hpp"} {
		data, err := os.ReadFile(fixturePath(t, "mathx", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
	}
	_ = source
	object := filepath.Join(dir, "mathx.o")
	output, err := exec.Command(compiler, "-std=c++17", "-c", "-o", object, source).CombinedOutput()
	require.NoErrorf(t, err, "compiling the fixture: %s", output)
	_, err = DefinedDynamicSymbols(object)
	assert.Error(t, err, "a .o file has no dynamic symbol table")
}

func isSorted(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}
