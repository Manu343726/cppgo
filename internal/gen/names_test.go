package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExportName checks the CamelCase rule DESIGN.md states.
func TestExportName(t *testing.T) {
	cases := map[string]string{
		"count_above": "CountAbove",
		"x_":          "X",
		"saxpy":       "Saxpy",
		"_leading":    "Leading",
		"a__b":        "AB",
		"":            "X",
		"2fast":       "X2fast",
		"already":     "Already",
		"x2_y":        "X2Y",
	}
	for in, want := range cases {
		assert.Equal(t, want, exportName(in), in)
	}
}

// TestTypeSuffix checks the suffix an overloaded name carries.
func TestTypeSuffix(t *testing.T) {
	cases := map[string]string{
		"float64":        "Float64",
		"int32":          "Int32",
		"[]float64":      "Float64Slice",
		"[][]int32":      "Int32SliceSlice",
		"*Vector":        "VectorPtr",
		"[]*Point":       "PointPtrSlice",
		"unsafe.Pointer": "UnsafePointer",
		"[4]float64":     "4Float64",
		"bool":           "Bool",
	}
	for in, want := range cases {
		assert.Equal(t, want, typeSuffix(in), in)
	}
}

// TestShortHash checks that the identifier is stable and that different
// signatures get different names.
func TestShortHash(t *testing.T) {
	first := shortHash("mathx::Vector::norm() const")
	assert.Equal(t, first, shortHash("mathx::Vector::norm() const"))
	assert.Len(t, first, 12)
	assert.NotEqual(t, first, shortHash("mathx::Vector::dot(const double *) const"))
}

// TestReceiverName checks the lowercase receiver, including the fallbacks.
func TestReceiverName(t *testing.T) {
	assert.Equal(t, "v", receiverName("Vector"))
	assert.Equal(t, "b", receiverName("BoxInt"))
	assert.Equal(t, "recv", receiverName(""))
	assert.Equal(t, "t", receiverName("Type"))
}

// TestParamName checks the declared-name rule and its fallbacks.
func TestParamName(t *testing.T) {
	cases := []struct {
		declared string
		index    int
		want     string
	}{
		{"value", 0, "value"},
		{"", 3, "arg3"},
		{"_", 1, "arg1"},
		{"type", 2, "arg2"},
		{"1x", 0, "_1x"},
		{"a b", 0, "a_b"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, paramName(tc.declared, tc.index), tc.declared)
	}
}

// TestUniqueName checks the collision suffix.
func TestUniqueName(t *testing.T) {
	taken := map[string]bool{}
	assert.Equal(t, "x", uniqueName("x", taken))
	assert.Equal(t, "x_2", uniqueName("x", taken))
	assert.Equal(t, "x_3", uniqueName("x", taken))
}

// TestCIdent checks the C parameter names, including keywords and digits.
func TestCIdent(t *testing.T) {
	cases := map[string]string{
		"":      "arg",
		"_":     "arg",
		"value": "value",
		"class": "class_",
		"1x":    "_1x",
		"a-b":   "a_b",
		"x":     "x",
	}
	for in, want := range cases {
		assert.Equal(t, want, cIdent(in), in)
	}
}

// TestGuardName checks the include guard.
func TestGuardName(t *testing.T) {
	assert.Equal(t, "MATHX", guardName("mathx"))
	assert.Equal(t, "A_B", guardName("a-b"))
	assert.Equal(t, "PKG_9", guardName("9"))
}

// TestValidGoPackage checks the package clause validation.
func TestValidGoPackage(t *testing.T) {
	assert.True(t, validGoPackage("mathx"))
	assert.True(t, validGoPackage("math_x"))
	assert.False(t, validGoPackage(""))
	assert.False(t, validGoPackage("9x"))
	assert.False(t, validGoPackage("type"))
	assert.False(t, validGoPackage("a-b"))
}

// TestValidGoIdentifier checks the rename target validation.
func TestValidGoIdentifier(t *testing.T) {
	assert.True(t, validGoIdentifier("Vector"))
	assert.True(t, validGoIdentifier("v_2"))
	assert.False(t, validGoIdentifier(""))
	assert.False(t, validGoIdentifier("9Vector"))
	assert.False(t, validGoIdentifier("func"))
	assert.False(t, validGoIdentifier("a-b"))
}

// TestSimpleName checks the last component of a qualified name and the
// unqualified case.
func TestSimpleName(t *testing.T) {
	assert.Equal(t, "Vector", simpleName("mathx::Vector"))
	assert.Equal(t, "Vector", simpleName("Vector"))
	assert.Equal(t, "norm", simpleName("a::b::norm"))
}

// TestGoRecordName checks the template-specialization naming rule from
// DESIGN.md.
func TestGoRecordName(t *testing.T) {
	cases := map[string]string{
		"mathx::Vector":               "Vector",
		"mathx::Box<int>":             "BoxInt",
		"mathx::Box<mathx::Vector>":   "BoxVector",
		"mathx::Pair<int, double>":    "PairIntFloat64",
		"tmpl::Cell<double>":          "CellFloat64",
		"mathx::Fixed<double, 4>":     "FixedFloat644",
		"mathx::Box<mathx::Box<int>>": "BoxBoxInt",
		"Box<bool>":                   "BoxBool",
		"Box<std::size_t>":            "BoxSize",
		"Box<int> extra":              "BoxInt",
	}
	for in, want := range cases {
		assert.Equal(t, want, goRecordName(in), in)
	}
}

// TestSplitTemplateArgs checks the top-level comma split.
func TestSplitTemplateArgs(t *testing.T) {
	assert.Equal(t, []string{"int"}, splitTemplateArgs("int"))
	assert.Equal(t, []string{"int", "double"}, splitTemplateArgs("int, double"))
	assert.Equal(t, []string{"Box<int, double>", "char"}, splitTemplateArgs("Box<int, double>, char"))
	assert.Empty(t, splitTemplateArgs(""))
}

// TestCPPArgGoName checks the argument names the specialization type uses.
func TestCPPArgGoName(t *testing.T) {
	cases := map[string]string{
		"int":             "Int",
		"unsigned int":    "Uint",
		"long":            "Long",
		"long long":       "LongLong",
		"bool":            "Bool",
		"char":            "Char",
		"unsigned char":   "Uchar",
		"float":           "Float32",
		"double":          "Float64",
		"const double":    "Float64",
		"volatile double": "Float64",
		"Box<int":         "Box<int",
		"std::size_t":     "Size",
		"std::uint64_t":   "Uint64",
		"int8_t":          "Int8",
		"4":               "4",
		"mathx::Vector":   "Vector",
		"Box<int>":        "BoxInt",
		"":                "X",
	}
	for in, want := range cases {
		assert.Equal(t, want, cppArgGoName(in), in)
	}
}
