package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShimSymbol maps a compiler diagnostic's line back to its thunk: that is
// the difference between "the shim does not compile" and "the thunk of
// ns::next_pow does not compile".
func TestShimSymbol(t *testing.T) {
	spans := []thunkSpan{{start: 10, end: 20, symbol: "A"}, {start: 22, end: 30, symbol: "B"}}
	assert.Equal(t, "A", shimSymbol(spans, 10))
	assert.Equal(t, "A", shimSymbol(spans, 15))
	assert.Equal(t, "B", shimSymbol(spans, 30))
	assert.Equal(t, ShimSourceFileName, shimSymbol(spans, 5))
	assert.Equal(t, ShimSourceFileName, shimSymbol(spans, 99))
	assert.Equal(t, ShimSourceFileName, shimSymbol(nil, 1))
}

// TestSplitSignature covers the spellings a function type is written in: the
// void parameter list, an empty one, and the two shapes that are not a function
// type at all.
func TestSplitSignature(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		result string
		params []string
		fails  bool
	}{
		{name: "two parameters", text: "std::int32_t(std::int32_t, std::int32_t)", result: "std::int32_t", params: []string{"std::int32_t", "std::int32_t"}},
		{name: "void parameter", text: "void(void)", result: "void"},
		{name: "no parameters", text: "double()", result: "double"},
		{name: "not a function", text: "double", fails: true},
		{name: "no closing parenthesis", text: "double(double", fails: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitSignature(tc.text)
			if tc.fails {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "is not a function type")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.result, got.result)
			assert.Equal(t, tc.params, got.params)
		})
	}
}

// TestFunctionSignature covers std::function's one template argument, including
// the two spellings the mapper refuses.
func TestFunctionSignature(t *testing.T) {
	got, err := functionSignature("std::function<std::int32_t(std::int32_t)>", "std::function")
	require.NoError(t, err)
	assert.Equal(t, "std::int32_t", got.result)
	assert.Equal(t, []string{"std::int32_t"}, got.params)

	_, err = functionSignature("std::function<int, int>", "std::function")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one-argument std::function")

	_, err = functionSignature("std::function", "std::function")
	require.Error(t, err)
}

// TestDeclErrorShapes pins the contract's one-line error and its two
// fallbacks: a file and a line, a line alone, and neither.
func TestDeclErrorShapes(t *testing.T) {
	assert.Equal(t, "cppgo: f.hpp:3: sym: what; fix", declError("f.hpp", 3, "sym", pending("what", "fix")).Error())
	assert.Equal(t, "cppgo: line 3: sym: what; fix", declError("", 3, "sym", pending("what", "fix")).Error())
	assert.Equal(t, "cppgo: <unknown>: sym: what; fix", declError("", 0, "sym", pending("what", "fix")).Error())
}
