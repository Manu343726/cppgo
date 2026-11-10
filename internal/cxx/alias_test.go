package cxx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestAliasedFunctionPointer resolves `using LogCallback = void (*)(...)`
// through the parameter that takes it: the pointee of a typedef of a function
// pointer is only visible in the canonical type, and the signature the
// generator reads comes from it.
func TestAliasedFunctionPointer(t *testing.T) {
	project, err := cxx.Open(cxx.Options{Header: "testdata/aliases/aliases.hpp"})
	require.NoError(t, err)
	defer func() { _ = project.Close() }()

	symbols := map[string]cxx.Symbol{}
	for _, symbol := range project.Symbols() {
		symbols[symbol.QualifiedName] = symbol
	}

	set := symbols["aliases::set_log_callback"]
	require.Len(t, set.Params, 1)
	assert.Equal(t, "LogCallback", set.Params[0].Spelling, "the declaration keeps the alias's name")
	assert.Equal(t, cxx.TypePointer, set.Params[0].Kind)
	require.NotNil(t, set.Params[0].Pointee, "a typedef of a function pointer has a pointee")
	assert.Equal(t, "FunctionProto", set.Params[0].Pointee.ClangKind)
	assert.Equal(t, "void (int, const char *)", set.Params[0].Pointee.Spelling)

	// The same shape written without the alias, which is what the alias must
	// resolve to: two declarations, one pointee spelling.
	raw := symbols["aliases::set_scalar_callback"]
	require.Len(t, raw.Params, 1)
	require.NotNil(t, raw.Params[0].Pointee)
	assert.Equal(t, "FunctionProto", raw.Params[0].Pointee.ClangKind)

	// The other side of the same rule: a typedef of `void *` keeps the invalid
	// pointee, because there is no canonical function prototype to resolve to
	// and no type Go could carry.
	handle := symbols["aliases::use_handle"]
	require.Len(t, handle.Params, 1)
	assert.Equal(t, "RawHandle", handle.Params[0].Spelling)
	require.NotNil(t, handle.Params[0].Pointee)
	assert.Equal(t, "Invalid", handle.Params[0].Pointee.ClangKind)

	value := symbols["aliases::set_value"]
	require.Len(t, value.Params, 1)
	assert.Equal(t, cxx.TypeFloat, value.Params[0].Kind, "a scalar alias resolves to its canonical kind")
	assert.Equal(t, "Scalar", value.Params[0].Spelling)
}
