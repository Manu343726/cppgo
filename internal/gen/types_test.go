package gen

import (
	"go/parser"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveSrc maps a type expression written as source text.
func resolveSrc(t *testing.T, src string) (*typeInfo, error) {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	require.NoError(t, err)
	return resolveType(expr)
}

// TestTypeMatrix pins the whole Go -> C mapping in one place.
func TestTypeMatrix(t *testing.T) {
	cases := []struct {
		goType string
		kind   kind
		slot   string
		size   int
		cType  string
	}{
		{"int8", kindInt, "int64", 8, "int8_t"},
		{"int16", kindInt, "int64", 8, "int16_t"},
		{"int32", kindInt, "int64", 8, "int32_t"},
		{"rune", kindInt, "int64", 8, "int32_t"},
		{"int64", kindInt, "int64", 8, "int64_t"},
		{"int", kindInt, "int64", 8, "int64_t"},
		{"uint8", kindInt, "uint64", 8, "uint8_t"},
		{"byte", kindInt, "uint64", 8, "uint8_t"},
		{"uint16", kindInt, "uint64", 8, "uint16_t"},
		{"uint32", kindInt, "uint64", 8, "uint32_t"},
		{"uint64", kindInt, "uint64", 8, "uint64_t"},
		{"uint", kindInt, "uint64", 8, "uint64_t"},
		{"uintptr", kindInt, "uintptr", 8, "uint64_t"},
		{"float32", kindFloat, "float32", 4, "float"},
		{"float64", kindFloat, "float64", 8, "double"},
		{"bool", kindBool, "int64", 8, "int32_t"},
		{"string", kindString, "", 0, "const char*"},
		{"[]float64", kindSlice, "", 0, "double*"},
		{"[]byte", kindSlice, "", 0, "uint8_t*"},
		{"[]int32", kindSlice, "", 0, "int32_t*"},
		{"*int32", kindPointer, "unsafe.Pointer", 8, "int32_t*"},
		{"*Point", kindPointer, "unsafe.Pointer", 8, "Point*"},
		{"cppgo.Handle", kindHandle, "unsafe.Pointer", 8, "void*"},
		{"error", kindError, "", 0, "int32_t"},
	}
	for _, tc := range cases {
		t.Run(tc.goType, func(t *testing.T) {
			info, err := resolveSrc(t, tc.goType)
			require.NoError(t, err)
			assert.Equal(t, tc.kind, info.kind)
			assert.Equal(t, tc.slot, info.slot)
			assert.Equal(t, tc.size, info.size)
			assert.Equal(t, tc.cType, info.cType)
			assert.Equal(t, tc.size, info.align())
		})
	}
}

func TestTypeMatrixElements(t *testing.T) {
	str, err := resolveSrc(t, "string")
	require.NoError(t, err)
	assert.Equal(t, "byte", str.elemGo)
	assert.Equal(t, "char", str.elemC)

	slice, err := resolveSrc(t, "[]float32")
	require.NoError(t, err)
	assert.Equal(t, "float32", slice.elemGo)
	assert.Equal(t, "float", slice.elemC)
	assert.Equal(t, "[]float32", slice.goType)

	ptr, err := resolveSrc(t, "*Point")
	require.NoError(t, err)
	assert.Equal(t, "Point", ptr.elemGo)
	assert.Equal(t, "Point", ptr.named)
	assert.Equal(t, "*Point", ptr.goType)

	builtin, err := resolveSrc(t, "*int32")
	require.NoError(t, err)
	assert.Empty(t, builtin.named)

	handle, err := resolveSrc(t, "cppgo.Handle")
	require.NoError(t, err)
	assert.Equal(t, "cppgo.Handle", handle.goType)
}

func TestResolveParentheses(t *testing.T) {
	info, err := resolveSrc(t, "(*int32)")
	require.NoError(t, err)
	assert.Equal(t, "int32_t*", info.cType)

	info, err = resolveSrc(t, "(float64)")
	require.NoError(t, err)
	assert.Equal(t, kindFloat, info.kind)
}

func TestResolveTypeErrors(t *testing.T) {
	cases := map[string]string{
		"[4]float64":       "only slices ([]T) cross the boundary",
		"interface{}":      "use cppgo.Handle to pass an opaque C++ object",
		"any":              "use cppgo.Handle to pass an opaque C++ object",
		"func()":           "callbacks from C++ into Go are not supported",
		"map[string]int":   "unsupported type",
		"chan int":         "unsupported type",
		"struct{ X int }":  "unsupported type",
		"[]string":         "only slices of numeric builtin types",
		"[]*int":           "only slices of numeric builtin types",
		"[]Point":          "only slices of numeric builtin types",
		"[][]byte":         "only slices of numeric builtin types",
		"**int":            "only pointers to builtin or named types",
		"*[]int":           "only pointers to builtin or named types",
		"*string":          "a pointer to string has no C representation",
		"*error":           "a pointer to error has no C representation",
		"*cppgo.Handle":    "pass cppgo.Handle directly",
		"*time.Time":       "only pointers to builtin types and to types of the bound package",
		"complex128":       "unsupported type complex128",
		"Celsius":          "unsupported type Celsius",
		"time.Time":        "unsupported type time.Time",
		"interface{ M() }": "unsupported type",
		"*(*int)":          "only pointers to builtin or named types",
		"[]complex64":      "only slices of numeric builtin types",
		"map[int]int":      "unsupported type",
		"<-chan *Point":    "unsupported type",
		"[]interface{}":    "only slices of numeric builtin types",
		"func(int) bool":   "callbacks from C++ into Go",
		"[3]byte":          "arrays must be passed as pointers",
		"[]bool":           "only slices of numeric builtin types",
		"[]error":          "only slices of numeric builtin types",
		"*[]byte":          "only pointers to builtin or named types",
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			_, err := resolveSrc(t, src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), want)
			assert.Contains(t, err.Error(), "unsupported type")
		})
	}
}

func TestASCIICoverageIsNotNeededForErrors(t *testing.T) {
	// The rejection message quotes the supported types back to the user.
	err := unsupportedType(mustExpr(t, "complex128"))
	assert.Contains(t, err.Error(), "int8/16/32/64")
	assert.Contains(t, err.Error(), "cppgo.Handle")
}
