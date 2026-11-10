package gen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// testMapper is a mapper with the two record shapes the type tests need.
func testMapper() *mapper {
	return &mapper{records: map[string]*recordModel{
		"mathx::Vector": {GoName: "Vector", CxxName: "mathx::Vector", Size: 24, Align: 8},
		"ns::Empty":     {GoName: "Empty", CxxName: "ns::Empty"},
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
			name:     "void pointer",
			param:    pointerParam(cxx.Param{Kind: cxx.TypeVoid, Spelling: "void", ClangKind: "Void"}, "void *", "Pointer"),
			wantGo:   "unsafe.Pointer",
			wantC:    "void *",
			wantKind: goPointer,
		},
		{
			// The canonical form of a `typedef void *` (`af_array`): the
			// pointee kind libclang reports is Invalid, and the declaration is
			// a void pointer, not an unknown type.
			name: "void pointer behind a typedef",
			param: pointerParam(
				cxx.Param{Kind: cxx.TypeOther, Spelling: "", ClangKind: "Invalid"},
				"const af_array", "Pointer"),
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

// TestMapRecord checks a mirrored record by value, a non-POD record by value,
// and the two failures.
func TestMapRecord(t *testing.T) {
	m := testMapper()
	point := cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Point", ClangKind: "Record", SizeBytes: 16, RecordName: "mathx::Point"}
	mapped, err := m.mapType(point)
	require.NoError(t, err)
	assert.Equal(t, goRecord, mapped.Kind)
	assert.Equal(t, "Point", mapped.Go)
	assert.False(t, mapped.ByValue, "a mirrored record is the Go struct, not an owned object")

	vector := cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Vector", ClangKind: "Record", SizeBytes: 16, RecordName: "mathx::Vector"}
	mapped, err = m.mapType(vector)
	require.NoError(t, err)
	assert.Equal(t, goPointer, mapped.Kind)
	assert.Equal(t, "*Vector", mapped.Go)
	assert.True(t, mapped.ByValue, "a record Go cannot mirror crosses as an owned object, copied by the shim")
	assert.Equal(t, "::mathx::Vector", mapped.Cxx)

	// A record whose layout libclang did not complete cannot be allocated, so a
	// by-value use of it names the limit rather than emitting an object of an
	// unknown size.
	_, err = m.mapType(cxx.Param{Kind: cxx.TypeRecord, Spelling: "ns::Empty", ClangKind: "Record", RecordName: "ns::Empty"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "passed by value but its layout is unknown")

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

	_, ok = stdTypeName(cxx.Param{Kind: cxx.TypeOther, Spelling: "std::deque<int>"})
	assert.False(t, ok, "only record and pointer declarations are library-shaped")
	_, ok = stdTypeName(cxx.Param{Kind: cxx.TypeInteger, Spelling: "std::int32_t", SizeBytes: 4})
	assert.False(t, ok, "a fixed-width alias is a scalar")
	sizePointer := cxx.Param{Kind: cxx.TypePointer, Ptr: true, Spelling: "std::size_t *",
		Pointee: &cxx.Param{Kind: cxx.TypeInteger, Spelling: "std::size_t", SizeBytes: 8}}
	_, ok = stdTypeName(sizePointer)
	assert.False(t, ok, "a pointer to a std scalar alias is a scalar pointer")
	rvalue := cxx.Param{Kind: cxx.TypePointer, Ptr: true, Spelling: "std::vector<double> &&",
		Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "std::vector<double>"}}
	name, ok = stdTypeName(rvalue)
	assert.True(t, ok)
	assert.Equal(t, "std::vector<double>", name)
}

// TestMissingRecordFix checks that a specialization error names -instantiate.
func TestMissingRecordFix(t *testing.T) {
	specialization := missingRecord(cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Box<int>", RecordName: "mathx::Box<int>"})
	assert.Contains(t, specialization.Error(), "-instantiate 'mathx::Box<int>'")
	plain := missingRecord(cxx.Param{Kind: cxx.TypeRecord, Spelling: "ns::Missing", RecordName: "ns::Missing"})
	assert.Contains(t, plain.Error(), "include its complete definition")
}

// TestMapStd covers every row of DESIGN.md's standard-library table, its
// malformed spellings, and the two smart-pointer shapes.
func TestMapStd(t *testing.T) {
	point := cxx.Param{Kind: cxx.TypeRecord, Spelling: "mathx::Point", ClangKind: "Record", RecordName: "mathx::Point"}
	cases := []struct {
		name    string
		param   cxx.Param
		wantGo  string
		wantStd stdKind
		wantErr string
	}{
		{"string", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::string"}, "string", stdString, ""},
		{"string_view", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::string_view"}, "string", stdStringView, ""},
		{"vector", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<double>"}, "[]float64", stdVector, ""},
		{"span", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::span<const double>"}, "[]float64", stdSpan, ""},
		{"array", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::array<int, 4>"}, "[4]int32", stdArray, ""},
		{"optional", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::optional<double>"}, "*float64", stdOptional, ""},
		{"optional record", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::optional<mathx::Point>"}, "*Point", stdOptional, ""},
		{"vector record", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<mathx::Point>"}, "[]Point", stdVector, ""},
		{"unique_ptr", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::unique_ptr<mathx::Vector>"}, "*Vector", stdUniquePtr, ""},
		{"shared_ptr", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::shared_ptr<mathx::Vector>"}, "*Vector", stdSharedPtr, ""},
		{"pointer to std", cxx.Param{Kind: cxx.TypePointer, Ptr: true, Spelling: "std::string *"}, "", stdNone, "pointer to a standard-library type"},
		{"unknown std", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::deque<int>"}, "", stdNone, "outside the emitted table"},
		{"vector two args", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<int, double>"}, "[]int32", stdVector, ""},
		{"vector unclosed", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<int"}, "", stdNone, "is not a std::vector"},
		{"array one arg", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::array<int>"}, "", stdNone, "two-argument"},
		{"array bad count", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::array<int, x>"}, "", stdNone, "is not an array size"},
		{"optional unknown", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::optional<ns::Missing>"}, "", stdNone, "not a scalar or a known record"},
		{"span two args", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::span<int, double>"}, "", stdNone, "one-argument"},
		{"array unclosed", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::array<int"}, "", stdNone, "is not a std::array"},
		{"array unknown", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::array<ns::Missing, 2>"}, "", stdNone, "not a scalar or a known record"},
		{"unique unclosed", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::unique_ptr<mathx::Vector"}, "", stdNone, "is not a std::unique_ptr"},
		{"vector empty args", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<>"}, "", stdNone, "no template arguments"},
		{"vector opaque", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<mathx::Vector>"}, "[]*Vector", stdVector, ""},
		{"unique unknown", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::unique_ptr<ns::Missing>"}, "", stdNone, "not part of the generated API"},
		{"shared two args", cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::shared_ptr<mathx::Vector, int>"}, "", stdNone, "one-argument"},
	}
	_ = point
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A fresh mapper per case: mapStd records the span header it needs.
			mapped, err := testMapper().mapStd(tc.param, mustStdName(t, tc.param))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantGo, mapped.Go)
			assert.Equal(t, tc.wantStd, mapped.Std)
		})
	}
}

// mustStdName returns the normalised standard-library name of a platform.
func mustStdName(t *testing.T, p cxx.Param) string {
	t.Helper()
	name, ok := stdTypeName(p)
	if !ok {
		// The pointer-to-std case is constructed directly.
		name = strings.TrimSuffix(strings.TrimSpace(p.Spelling), " *")
	}
	return name
}

// TestLookupRecordAmbiguous checks the unqualified-spelling fallback's
// ambiguity guard.
func TestLookupRecordAmbiguous(t *testing.T) {
	m := &mapper{records: map[string]*recordModel{
		"a::V": {GoName: "AV"},
		"b::V": {GoName: "BV"},
	}}
	_, ok := m.lookupRecord("V")
	assert.False(t, ok, "an unqualified name that matches two records is ambiguous")
	record, ok := m.lookupRecord("a::V")
	assert.True(t, ok)
	assert.Equal(t, "AV", record.GoName)
}

// TestMapElementRefusesAnElementAContainerCannotHold covers the recursive
// element map's two refusals: a standard-library element the wire cannot carry
// (a string needs a character pool), and a type the model knows nothing about.
func TestMapElementRefusesAnElementAContainerCannotHold(t *testing.T) {
	m := &mapper{}
	_, err := m.mapElement("std::vector<std::string>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "standard-library element")
	assert.Contains(t, err.Error(), "std::vector of those")

	_, err = m.mapElement("ns::Unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a scalar or a known record")
}

// TestCallbackElementReportsTheRefusal covers the callback argument map's
// propagation: an element the model refuses is the callback's refusal, named
// with the same reason.
func TestCallbackElementReportsTheRefusal(t *testing.T) {
	m := &mapper{}
	_, err := m.callbackElement("std::vector<std::string>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "standard-library element")
}
