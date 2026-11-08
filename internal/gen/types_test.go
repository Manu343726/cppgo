package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// testMapper is a mapper with the two record shapes the type tests need.
func testMapper() *mapper {
	return &mapper{records: map[string]*recordModel{
		"mathx::Vector": {GoName: "Vector", CxxName: "mathx::Vector"},
		"mathx::Point":  {GoName: "Point", CxxName: "mathx::Point", Mirrored: true},
	}}
}

// integerParam builds an integer parameter of the given width.
func integerParam(size int64, unsigned bool, clang string) cxx.Param {
	return cxx.Param{Kind: cxx.TypeInteger, Spelling: clang, ClangKind: clang, SizeBytes: size, Unsigned: unsigned}
}

// pointerParam builds a pointer or reference parameter.
func pointerParam(pointee cxx.Param, spelling, clang string) cxx.Param {
	return cxx.Param{Kind: cxx.TypePointer, Spelling: spelling, ClangKind: "Pointer", SizeBytes: 8, Ptr: true, Pointee: &pointee}
}

// TestMapInteger checks every width and the LP64 guard.
func TestMapInteger(t *testing.T) {
	m := testMapper()
	cases := []struct {
		name     string
		param    cxx.Param
		want     string
		wantC    string
		wantErr  string
		unsigned bool
	}{
		{"int8", integerParam(1, false, "SChar"), "int8", "int8_t", "", false},
		{"uint8", integerParam(1, true, "UChar"), "uint8", "uint8_t", "", true},
		{"int16", integerParam(2, false, "Short"), "int16", "int16_t", "", false},
		{"uint16", integerParam(2, true, "UShort"), "uint16", "uint16_t", "", true},
		{"int32", integerParam(4, false, "Int"), "int32", "int32_t", "", false},
		{"uint32", integerParam(4, true, "UInt"), "uint32", "uint32_t", "", true},
		{"int64", integerParam(8, false, "Long"), "int64", "int64_t", "", false},
		{"uint64", integerParam(8, true, "ULong"), "uint64", "uint64_t", "", true},
		{"long not lp64", integerParam(4, false, "Long"), "", "", "LP64", false},
		{"bad width", integerParam(3, false, "Int"), "", "", "no Go value type", false},
		{"bool", cxx.Param{Kind: cxx.TypeInteger, Spelling: "bool", ClangKind: "Bool", SizeBytes: 1}, "bool", "bool", "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mapped, err := m.mapType(tc.param)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, mapped.Go)
			assert.Equal(t, tc.wantC, mapped.C)
			assert.Equal(t, tc.unsigned, mapped.Param.Unsigned && mapped.Kind != goBool)
		})
	}
}

// TestMapFloat checks float, double and the unrepresentable long double.
func TestMapFloat(t *testing.T) {
	m := testMapper()
	cases := []struct {
		name    string
		param   cxx.Param
		want    string
		wantErr string
	}{
		{"float", cxx.Param{Kind: cxx.TypeFloat, Spelling: "float", ClangKind: "Float", SizeBytes: 4, Float: true}, "float32", ""},
		{"double", cxx.Param{Kind: cxx.TypeFloat, Spelling: "double", ClangKind: "Double", SizeBytes: 8, Float: true}, "float64", ""},
		{"long double", cxx.Param{Kind: cxx.TypeFloat, Spelling: "long double", ClangKind: "LongDouble", SizeBytes: 16, Float: true}, "", "long double"},
		{"long double 8", cxx.Param{Kind: cxx.TypeFloat, Spelling: "long double", ClangKind: "LongDouble", SizeBytes: 8, Float: true}, "", "long double"},
		{"bad float", cxx.Param{Kind: cxx.TypeFloat, Spelling: "half", ClangKind: "Half", SizeBytes: 2, Float: true}, "", "no Go value type"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mapped, err := m.mapType(tc.param)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, mapped.Go)
		})
	}
}

// TestMapPointer checks scalar pointers, record pointers and the two ways a
// record pointer is represented.
func TestMapPointer(t *testing.T) {
	m := testMapper()
	double := cxx.Param{Kind: cxx.TypeFloat, Spelling: "double", ClangKind: "Double", SizeBytes: 8, Float: true}
	cases := []struct {
		name     string
		param    cxx.Param
		wantGo   string
		wantC    string
		wantErr  string
		wantKind goKind
	}{
		{
			name:     "scalar pointer",
			param:    pointerParam(double, "double *", "Pointer"),
			wantGo:   "*float64",
			wantC:    "double *",
			wantKind: goPointer,
		},
		{
			name: "const scalar pointer",
			param: func() cxx.Param {
				constDouble := double
				constDouble.IsConst = true
				return pointerParam(constDouble, "const double *", "Pointer")
			}(),
			wantGo:   "*float64",
			wantC:    "const double *",
			wantKind: goPointer,
		},
		{
			name: "opaque record pointer",
			param: pointerParam(
				cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Vector", ClangKind: "Record", SizeBytes: 16, RecordName: "mathx::Vector"},
				"mathx::Vector *", "Pointer"),
			wantGo:   "*Vector",
			wantC:    "void *",
			wantKind: goPointer,
		},
		{
			name: "mirrored record pointer",
			param: pointerParam(
				cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Point", ClangKind: "Record", SizeBytes: 16, RecordName: "mathx::Point"},
				"mathx::Point *", "Pointer"),
			wantGo:   "*Point",
			wantC:    "void *",
			wantKind: goPointer,
		},
		{
			name:    "void pointer",
			param:   pointerParam(cxx.Param{Kind: cxx.TypeVoid, Spelling: "void", ClangKind: "Void"}, "void *", "Pointer"),
			wantErr: "void pointer",
		},
		{
			name:    "pointer libclang did not describe",
			param:   cxx.Param{Kind: cxx.TypePointer, Spelling: "mystery *", Ptr: true},
			wantErr: "libclang did not describe",
		},
		{
			name:    "nullptr",
			param:   cxx.Param{Kind: cxx.TypePointer, Spelling: "std::nullptr_t", ClangKind: "NullPtr", Ptr: true},
			wantErr: "nullptr_t",
		},
		{
			name: "unknown record",
			param: pointerParam(
				cxx.Param{Kind: cxx.TypeRecord, Spelling: "ns::Missing", ClangKind: "Record", RecordName: "ns::Missing"},
				"ns::Missing *", "Pointer"),
			wantErr: "not part of the generated API",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mapped, err := m.mapType(tc.param)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantGo, mapped.Go)
			assert.Equal(t, tc.wantC, mapped.C)
			assert.Equal(t, tc.wantKind, mapped.Kind)
		})
	}
}

// TestMapRecord checks a mirrored record by value and the two failures.
func TestMapRecord(t *testing.T) {
	m := testMapper()
	point := cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Point", ClangKind: "Record", SizeBytes: 16, RecordName: "mathx::Point"}
	mapped, err := m.mapType(point)
	require.NoError(t, err)
	assert.Equal(t, goRecord, mapped.Kind)
	assert.Equal(t, "Point", mapped.Go)

	vector := cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Vector", ClangKind: "Record", SizeBytes: 16, RecordName: "mathx::Vector"}
	_, err = m.mapType(vector)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "passed by value but is not a POD")

	_, err = m.mapType(cxx.Param{Kind: cxx.TypeRecord, Spelling: "ns::Unknown", ClangKind: "Record", RecordName: "ns::Unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not part of the generated API")
}

// TestMapUnsupported covers the kinds phase 2b owns, which must name themselves.
func TestMapUnsupported(t *testing.T) {
	m := testMapper()
	cases := map[string]cxx.Param{
		"is an enum":   {Kind: cxx.TypeEnum, Spelling: "mathx::Axis", ClangKind: "Enum"},
		"is a C array": {Kind: cxx.TypeArray, Spelling: "int[4]", ClangKind: "ConstantArray"},
		"not a type":   {Kind: cxx.TypeOther, Spelling: "__int128", ClangKind: "Int128"},
		"void":         {Kind: cxx.TypeVoid, Spelling: "void", ClangKind: "Void"},
	}
	for want, param := range cases {
		mapped, err := m.mapType(param)
		if want == "void" {
			require.NoError(t, err)
			assert.Equal(t, goVoid, mapped.Kind)
			continue
		}
		require.Error(t, err)
		assert.Contains(t, err.Error(), want)
	}
}

// TestStdTypeName checks the spelling-based standard-library detection that
// runs before libclang's canonical form, which can be wrong.
func TestStdTypeName(t *testing.T) {
	vector := cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<double>", ClangKind: "Record"}
	name, ok := stdTypeName(vector)
	assert.True(t, ok)
	assert.Equal(t, "std::vector<double>", name)

	volatileRef := cxx.Param{
		Kind: cxx.TypePointer, Ptr: true, Spelling: "volatile std::string &",
		Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "std::string"},
	}
	name, ok = stdTypeName(volatileRef)
	assert.True(t, ok)
	assert.Equal(t, "std::string", name)

	stringRef := cxx.Param{
		Kind: cxx.TypePointer, Ptr: true, Spelling: "const std::string &",
		Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "std::string"},
	}
	name, ok = stdTypeName(stringRef)
	assert.True(t, ok)
	assert.Equal(t, "std::string", name)

	scalar := cxx.Param{Kind: cxx.TypeInteger, Spelling: "std::size_t", SizeBytes: 8}
	_, ok = stdTypeName(scalar)
	assert.False(t, ok, "std::size_t is a scalar, not a library type")

	nullptr := cxx.Param{
		Kind: cxx.TypePointer, Ptr: true, Spelling: "std::nullptr_t",
		Pointee: &cxx.Param{Kind: cxx.TypeRecord},
	}
	_, ok = stdTypeName(nullptr)
	assert.False(t, ok, "nullptr_t has its own error")

	_, ok = stdTypeName(cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Vector"})
	assert.False(t, ok)
}

// TestMissingRecordFix checks that a specialisation error names -instantiate.
func TestMissingRecordFix(t *testing.T) {
	specialization := missingRecord(cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Box<int>", RecordName: "mathx::Box<int>"})
	assert.Contains(t, specialization.Error(), "-instantiate 'mathx::Box<int>'")
	plain := missingRecord(cxx.Param{Kind: cxx.TypeRecord, Spelling: "ns::Missing", RecordName: "ns::Missing"})
	assert.Contains(t, plain.Error(), "include its complete definition")
}
