package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// parseABI parses a one-class, one-function abi package without resolving it
// against a header: every test here supplies the C++ side itself, which is what
// makes the validation rules testable one branch at a time.
func parseABI(t *testing.T, body string) *packageInfo {
	t.Helper()
	dir := writeABIPkg(t, "/nonexistent/vec.hpp", `//cppgo:class ns::Vec
type Vec cppgo.Handle

`+body)
	pkg, err := parsePackage(dir)
	require.NoError(t, err)
	return pkg
}

// abiFunc0 is the single bound function of parseABI's package.
func abiFunc0(t *testing.T, body string) *Func {
	t.Helper()
	pkg := parseABI(t, body)
	require.Len(t, pkg.Funcs, 1)
	return pkg.Funcs[0]
}

// intParam is an int parameter, i.e. four bytes in one integer register.
func intParam() cxx.Param {
	return cxx.Param{Spelling: "int", Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}
}

// doubleParam is a double parameter, i.e. eight bytes in one XMM register.
func doubleParam() cxx.Param {
	return cxx.Param{Spelling: "double", Kind: cxx.TypeFloat, ClangKind: "Double", SizeBytes: 8, Float: true}
}

func TestMatchParam(t *testing.T) {
	cases := []struct {
		name   string
		goType string
		c      cxx.Param
		want   string
	}{
		{"int matches int", "int32", intParam(), ""},
		{"int8 matches char", "int8", cxx.Param{Spelling: "char", Kind: cxx.TypeInteger, SizeBytes: 1}, ""},
		{"width mismatch", "int64", intParam(), "the Go type is int64 (8-byte) but the C++ type is \"int\" (4-byte)"},
		{"enum rides in an integer", "int32", cxx.Param{Spelling: "Axis", Kind: cxx.TypeEnum, SizeBytes: 1}, ""},
		{"enum wider than the Go type", "int8", cxx.Param{Spelling: "Wide", Kind: cxx.TypeEnum, SizeBytes: 4}, "the Go type is int8 (1-byte)"},
		{"float against int", "float64", intParam(), "travels in the other register class"},
		{"int against double", "int32", doubleParam(), "travels in the other register class"},
		{"double matches", "float64", doubleParam(), ""},
		{"float32 against double", "float32", doubleParam(), "the Go type is float32 (4-byte)"},
		{"bool matches", "bool", cxx.Param{Spelling: "bool", Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1}, ""},
		{"bool against int", "bool", intParam(), "bool crosses as a C++ bool only"},
		{"handle against pointer", "cppgo.Handle", cxx.Param{Spelling: "Item *", Kind: cxx.TypePointer, Ptr: true, SizeBytes: 8}, ""},
		{"pointer against value", "*int32", intParam(), "a pointer, but the C++ parameter is \"int\", passed by value"},
		{"slice against pointer", "[]float64", cxx.Param{Spelling: "const double *", Kind: cxx.TypePointer, Ptr: true, SizeBytes: 8}, ""},
		{"slice against value", "[]float64", doubleParam(), "crosses as (pointer, length)"},
		{"string against value", "string", intParam(), "crosses as (pointer, length)"},
		{"object handle", "Vec", cxx.Param{Spelling: "Vec *", Kind: cxx.TypePointer, Ptr: true}, "is the object pointer, not an argument"},
		{"record by value", "int32", cxx.Param{Spelling: "Pod", Kind: cxx.TypeRecord, SizeBytes: 8}, "a struct passed by value"},
		{"std type", "int32", cxx.Param{Spelling: "std::string", Kind: cxx.TypeRecord, SizeBytes: 32}, "a standard-library type"},
		{"unclassifiable", "int32", cxx.Param{Spelling: "int Vec::*", Kind: cxx.TypeOther}, "cannot classify"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := resolveIn(map[string]*classInfo{"Vec": {GoName: "Vec", CxxName: "ns::Vec"}}, mustExpr(t, tc.goType))
			require.NoError(t, err)
			got := matchParam(info, tc.c)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestMatchResult(t *testing.T) {
	cases := []struct {
		name   string
		goType string
		c      cxx.Param
		want   string
	}{
		{"void", "int32", cxx.Param{Spelling: "void", Kind: cxx.TypeVoid}, "returns void, but the binding declares"},
		{"std", "int32", cxx.Param{Spelling: "std::vector<int>", Kind: cxx.TypeRecord}, "a standard-library type"},
		{"unclassifiable", "int32", cxx.Param{Spelling: "int Vec::*", Kind: cxx.TypeOther}, "cannot classify"},
		{"int matches", "int32", intParam(), ""},
		{"int width", "int64", intParam(), "8-byte"},
		{"enum", "int32", cxx.Param{Spelling: "Axis", Kind: cxx.TypeEnum, SizeBytes: 1}, ""},
		{"enum too wide", "int8", cxx.Param{Spelling: "Axis", Kind: cxx.TypeEnum, SizeBytes: 4}, "1-byte"},
		{"int against double", "int32", doubleParam(), "register class"},
		{"double matches", "float64", doubleParam(), ""},
		{"float32 against double", "float32", doubleParam(), "register class"},
		{"bool matches", "bool", cxx.Param{Spelling: "bool", Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1}, ""},
		{"bool against int", "bool", intParam(), "the Go result is bool"},
		{"pointer matches", "*int32", cxx.Param{Spelling: "const char *", Kind: cxx.TypePointer, Ptr: true}, ""},
		{"pointer against value", "*int32", intParam(), "returns \"int\" by value"},
		{"object", "Vec", intParam(), "cannot be returned by value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := resolveIn(map[string]*classInfo{"Vec": {GoName: "Vec", CxxName: "ns::Vec"}}, mustExpr(t, tc.goType))
			require.NoError(t, err)
			got := matchResult(info, tc.c)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestMatchLength(t *testing.T) {
	assert.Empty(t, matchLength(cxx.Param{Spelling: "std::size_t", Kind: cxx.TypeInteger, SizeBytes: 8}))
	assert.Contains(t, matchLength(intParam()), "passes its length as a size_t")
}

func TestResultShape(t *testing.T) {
	ctor := abiFunc0(t, "//cppgo:ctor\nfunc New() (Vec, error)")
	shape, why := resultShape(ctor, cxx.Symbol{})
	assert.Equal(t, abiObject, shape)
	assert.Empty(t, why)

	structResult := abiFunc0(t, "//cppgo:method scaled\nfunc (v Vec) Scaled(k float64) Vec")
	shape, why = resultShape(structResult, cxx.Symbol{Result: cxx.Param{Spelling: "Vec", Kind: cxx.TypeRecord, SizeBytes: 24}, HasSret: true})
	assert.Equal(t, shapeFatal, shape)
	assert.Contains(t, why, "returns Vec by value")
	assert.Contains(t, why, "shim mode")

	value := abiFunc0(t, "//cppgo:method norm\nfunc (v Vec) Norm() float64")
	shape, why = resultShape(value, cxx.Symbol{Result: doubleParam()})
	assert.Equal(t, abiValue, shape)
	assert.Empty(t, why)

	valueError := abiFunc0(t, "//cppgo:method dot\nfunc (v Vec) Dot() (float64, error)")
	shape, why = resultShape(valueError, cxx.Symbol{Result: doubleParam()})
	assert.Equal(t, abiValueError, shape)
	assert.Empty(t, why)

	mismatch := abiFunc0(t, "//cppgo:method norm\nfunc (v Vec) Norm() float64")
	shape, why = resultShape(mismatch, cxx.Symbol{Result: intParam()})
	assert.Equal(t, abiValue, shape)
	assert.Contains(t, why, "register class")

	status := abiFunc0(t, "//cppgo:function ns::check\nfunc Check() error")
	shape, why = resultShape(status, cxx.Symbol{Result: intParam()})
	assert.Equal(t, abiStatus, shape)
	assert.Empty(t, why)

	enumStatus := abiFunc0(t, "//cppgo:function ns::check\nfunc Check() error")
	shape, _ = resultShape(enumStatus, cxx.Symbol{Result: cxx.Param{Spelling: "Axis", Kind: cxx.TypeEnum, SizeBytes: 1}})
	assert.Equal(t, abiStatus, shape)

	voidError := abiFunc0(t, "//cppgo:function ns::run\nfunc Run() error")
	shape, why = resultShape(voidError, cxx.Symbol{Result: cxx.Param{Spelling: "void", Kind: cxx.TypeVoid}})
	assert.Equal(t, abiErrorOnly, shape)
	assert.Empty(t, why)

	floatError := abiFunc0(t, "//cppgo:function ns::ratio\nfunc Ratio() error")
	shape, why = resultShape(floatError, cxx.Symbol{Result: doubleParam()})
	assert.Equal(t, abiErrorOnly, shape)
	assert.Contains(t, why, "needs an integer return type")

	void := abiFunc0(t, "//cppgo:method reset\nfunc (v Vec) Reset()")
	shape, why = resultShape(void, cxx.Symbol{Result: cxx.Param{Spelling: "double", Kind: cxx.TypeFloat}})
	assert.Equal(t, abiVoid, shape)
	assert.Empty(t, why, "a scalar result the binding discards is still callable")
}

func TestKindProblems(t *testing.T) {
	method := abiFunc0(t, "//cppgo:method norm\nfunc (v Vec) Norm() float64")
	assert.Empty(t, kindProblems(method, cxx.Symbol{Kind: cxx.Method}))
	problems := kindProblems(method, cxx.Symbol{Kind: cxx.StaticMethod, QualifiedName: "ns::Vec::dims"})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].msg, "static member function, which has no this")
	assert.Contains(t, problems[0].msg, "//cppgo:function ns::Vec::dims")
	problems = kindProblems(method, cxx.Symbol{Kind: cxx.FreeFunc})
	assert.Contains(t, problems[0].msg, "it is a free function, not a member function")

	ctor := abiFunc0(t, "//cppgo:ctor\nfunc New() (Vec, error)")
	assert.Empty(t, kindProblems(ctor, cxx.Symbol{Kind: cxx.Constructor}))
	problems = kindProblems(ctor, cxx.Symbol{Kind: cxx.Method})
	assert.Contains(t, problems[0].msg, "not a constructor")
	assert.Contains(t, problems[0].msg, "ns::Vec::Vec")

	dtor := abiFunc0(t, "//cppgo:dtor\nfunc (v Vec) Destroy()")
	assert.Empty(t, kindProblems(dtor, cxx.Symbol{Kind: cxx.Destructor}))
	problems = kindProblems(dtor, cxx.Symbol{Kind: cxx.Method})
	assert.Contains(t, problems[0].msg, "not a destructor")
	assert.Contains(t, problems[0].msg, "ns::Vec::~Vec")

	free := abiFunc0(t, "//cppgo:function ns::f\nfunc F() int32")
	assert.Empty(t, kindProblems(free, cxx.Symbol{Kind: cxx.FreeFunc}))
	assert.Empty(t, kindProblems(free, cxx.Symbol{Kind: cxx.StaticMethod}))
	problems = kindProblems(free, cxx.Symbol{Kind: cxx.Method})
	assert.Contains(t, problems[0].msg, "needs this")
}

func TestPickOverload(t *testing.T) {
	f := abiFunc0(t, "//cppgo:function ns::add\nfunc Add(a, b int32) int32")
	intOverload := cxx.Symbol{QualifiedName: "ns::add", Mangled: "_ZN2ns3addEii", Params: []cxx.Param{intParam(), intParam()}, Result: intParam()}
	doubleOverload := cxx.Symbol{QualifiedName: "ns::add", Mangled: "_ZN2ns3addEdd", Params: []cxx.Param{doubleParam(), doubleParam()}, Result: doubleParam()}

	sym, sameArity, err := pickOverload(f, []cxx.Symbol{intOverload, doubleOverload})
	require.NoError(t, err)
	assert.Equal(t, intOverload.Mangled, sym.Mangled)
	assert.Len(t, sameArity, 2)

	// Nothing takes the right number of arguments.
	one := abiFunc0(t, "//cppgo:function ns::one\nfunc One(a int32) int32")
	_, sameArity, err = pickOverload(one, []cxx.Symbol{intOverload})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no overload takes 1 argument;")
	assert.Empty(t, sameArity)

	// The right arity, the wrong types.
	_, sameArity, err = pickOverload(f, []cxx.Symbol{{QualifiedName: "ns::add", Mangled: "_ZN2ns3addEbb", Params: []cxx.Param{
		{Spelling: "bool", Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1},
		{Spelling: "bool", Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1},
	}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no overload accepts the Go argument types")
	assert.Contains(t, err.Error(), "ns::add(bool, bool)")
	assert.Len(t, sameArity, 1)

	// Two overloads that both accept the Go types are a real ambiguity.
	dup := cxx.Symbol{QualifiedName: "ns::add", Mangled: "_ZN2ns3addEii2", Params: []cxx.Param{intParam(), intParam()}}
	_, _, err = pickOverload(f, []cxx.Symbol{intOverload, dup})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 overloads accept the Go argument types")
	assert.Contains(t, err.Error(), "unambiguous")

	// A slice spends two C++ parameters, so dot(const double*, size_t) fits a
	// Go []float64.
	dot := abiFunc0(t, "//cppgo:method dot\nfunc (v Vec) Dot(ys []float64) float64")
	sym, _, err = pickOverload(dot, []cxx.Symbol{{QualifiedName: "ns::Vec::dot", Mangled: "_Z", Params: []cxx.Param{
		{Spelling: "const double *", Kind: cxx.TypePointer, Ptr: true, SizeBytes: 8},
		{Spelling: "std::size_t", Kind: cxx.TypeInteger, SizeBytes: 8},
	}}})
	require.NoError(t, err)
	assert.Equal(t, "_Z", sym.Mangled)
}

func TestParamReasonsArity(t *testing.T) {
	f := abiFunc0(t, "//cppgo:function ns::f\nfunc F(a, b int32)")
	reasons := paramReasons(f, cxx.Symbol{Params: []cxx.Param{intParam()}})
	require.Len(t, reasons, 1)
	assert.Contains(t, reasons[0], "the binding describes 2 arguments but the C++ signature takes 1 argument")

	f = abiFunc0(t, "//cppgo:function ns::f\nfunc F(s string)")
	reasons = paramReasons(f, cxx.Symbol{Params: []cxx.Param{
		{Spelling: "const char *", Kind: cxx.TypePointer, Ptr: true},
		{Spelling: "int", Kind: cxx.TypeInteger, SizeBytes: 4},
	}})
	require.Len(t, reasons, 1)
	assert.Contains(t, reasons[0], "passes its length as a size_t")
}

func TestSignatures(t *testing.T) {
	assert.Equal(t, "1 argument", argumentCount(1))
	assert.Equal(t, "3 arguments", argumentCount(3))
	syms := []cxx.Symbol{
		{QualifiedName: "ns::add", Params: []cxx.Param{intParam(), intParam()}},
		{QualifiedName: "ns::add", Params: []cxx.Param{doubleParam()}},
	}
	assert.Equal(t, "ns::add(int, int), ns::add(double)", signatures(syms))
}

func TestCxxName(t *testing.T) {
	pkg := parseABI(t, `//cppgo:ctor
func New() (Vec, error)

//cppgo:method norm
func (v Vec) Norm() float64

//cppgo:dtor
func (v Vec) Destroy()

//cppgo:function ns::helpers::make
func Make() int32
`)
	names := map[string]string{}
	for _, f := range pkg.Funcs {
		names[f.Name] = f.cxxName()
	}
	assert.Equal(t, "ns::Vec::Vec", names["New"])
	assert.Equal(t, "ns::Vec::norm", names["Norm"])
	assert.Equal(t, "ns::Vec::~Vec", names["Destroy"])
	assert.Equal(t, "ns::helpers::make", names["Make"])
}

func TestCheckUniqueSymbols(t *testing.T) {
	pkg := parseABI(t, `//cppgo:function ns::add
func A(a, b int32) int32

//cppgo:function ns::add
func B(a, b int32) int32
`)
	for _, f := range pkg.Funcs {
		f.Symbol = "_ZN2ns3addEii"
	}
	err := checkUniqueSymbols(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both bind _ZN2ns3addEii")
}

func TestABIReport(t *testing.T) {
	pkg := parseABI(t, `//cppgo:method norm
func (v Vec) Norm() float64
`)
	f := pkg.Funcs[0]
	f.abi = &abiBinding{
		sym:    cxx.Symbol{QualifiedName: "ns::Vec::norm", Mangled: "_ZNK2ns3Vec4normEv"},
		notes:  []string{"a note"},
		forced: []string{"a forced problem"},
	}
	report := (&abiPackage{source: "compile_commands.json"}).report(pkg.Funcs)
	require.Len(t, report, 1)
	assert.Contains(t, report[0], "ns::Vec::norm")
	assert.Contains(t, report[0], "mangled _ZNK2ns3Vec4normEv")
	assert.Contains(t, report[0], "abi from compile_commands.json")
	assert.Contains(t, report[0], "note: a note")
	assert.Contains(t, report[0], "forced with -force, NOT validated: a forced problem")

	assumed := (&abiPackage{source: "fallback", assumed: true}).report(pkg.Funcs)
	assert.Contains(t, assumed[0], "abi from fallback flags (assumed)")
}

func TestABIValueAndErrorHelpers(t *testing.T) {
	value := abiFunc0(t, "//cppgo:method norm\nfunc (v Vec) Norm() float64")
	assert.NotNil(t, valueResult(value))
	assert.False(t, hasErrorResult(value))

	status := abiFunc0(t, "//cppgo:function ns::check\nfunc Check() error")
	assert.Nil(t, valueResult(status))
	assert.True(t, hasErrorResult(status))

	none := abiFunc0(t, "//cppgo:dtor\nfunc (v Vec) Destroy()")
	assert.Nil(t, valueResult(none))
	assert.False(t, hasErrorResult(none))
}
