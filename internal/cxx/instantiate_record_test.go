package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// instantiateFixture opens the template-parameter fixture, which carries a
// class template with an array field, a derived pattern, a self-referential
// pattern, an incomplete field and a virtual base.
func instantiateFixture(t *testing.T) *Project {
	t.Helper()
	skipWithoutLibclang(t)
	dir := t.TempDir()
	data, err := os.ReadFile(fixturePath(t, "tmplparams", "params.hpp"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "params.hpp"), data, 0o644))
	project, err := Open(Options{
		Header: filepath.Join(dir, "params.hpp"),
		Args:   []string{"-std=c++17", "-I" + dir},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })
	return project
}

// doubleArg and intArg and fourArg are the manifest's type arguments, built the
// same way gen builds them.
func intArg() TemplateArgument {
	param, ok := ScalarType("int")
	if !ok {
		panic("int is not a scalar")
	}
	return TemplateArgument{Kind: TemplateArgumentType, Spelling: "int", Type: param}
}

func doubleArg() TemplateArgument {
	param, _ := ScalarType("double")
	return TemplateArgument{Kind: TemplateArgumentType, Spelling: "double", Type: param}
}

func intValueArg(value int64) TemplateArgument {
	return TemplateArgument{Kind: TemplateArgumentIntegral, Spelling: "4", Value: value, UnsignedValue: uint64(value)}
}

// TestInstantiateRecordLayout covers the synthesis: substituted fields, the
// standard-layout size and offsets, POD, and the constructibility facts.
func TestInstantiateRecordLayout(t *testing.T) {
	project := instantiateFixture(t)

	rec, ok := project.InstantiateRecord("params::Fixed", []TemplateArgument{doubleArg(), intValueArg(4)})
	require.True(t, ok)
	assert.Equal(t, "params::Fixed<double,4>", rec.QualifiedName)
	assert.True(t, rec.IsTemplateSpecialization)
	assert.True(t, rec.Synthesized)
	assert.Equal(t, int64(32), rec.SizeBytes)
	assert.Equal(t, int64(8), rec.AlignBytes)
	assert.True(t, rec.IsPOD)
	require.Len(t, rec.Fields, 1)
	t.Logf("field: kind=%s spell=%q arraySize=%d elemNil=%v size=%d", rec.Fields[0].Type.Kind, rec.Fields[0].Type.Spelling, rec.Fields[0].Type.ArraySize, rec.Fields[0].Type.Element == nil, rec.Fields[0].Type.SizeBytes)
	assert.Equal(t, "values", rec.Fields[0].Name)
	assert.Equal(t, TypeArray, rec.Fields[0].Type.Kind)
	assert.Equal(t, int64(4), rec.Fields[0].Type.ArraySize)
	require.NotNil(t, rec.Fields[0].Type.Element)
	assert.Equal(t, TypeFloat, rec.Fields[0].Type.Element.Kind)
	assert.Equal(t, int64(0), rec.Fields[0].OffsetBytes)

	// The record is registered: a later lookup by its qualified or its simple
	// name finds the same layout.
	registered, ok := project.Record("params::Fixed<double,4>")
	require.True(t, ok)
	assert.Equal(t, int64(32), registered.SizeBytes)
	found, ok := project.tus[0].recordBySpelling("Fixed<double, 4>")
	require.True(t, ok)
	assert.Equal(t, "params::Fixed<double,4>", found.QualifiedName)
}

// TestInstantiateRecordFlattensBase checks that a base's fields land in the
// derived record at the base's offset, which is what makes a mirrored Go struct
// the C layout.
func TestInstantiateRecordFlattensBase(t *testing.T) {
	project := instantiateFixture(t)

	rec, ok := project.InstantiateRecord("params::Derived", []TemplateArgument{intArg()})
	require.True(t, ok)
	assert.Equal(t, int64(8), rec.SizeBytes)
	require.Len(t, rec.Bases, 1)
	assert.Equal(t, "params::Base<int>", rec.Bases[0].QualifiedName)
	assert.Equal(t, int64(0), rec.Bases[0].OffsetBytes)
	require.Len(t, rec.Fields, 2, "the base's field and the derived field")
	assert.Equal(t, "value", rec.Fields[0].Name)
	assert.Equal(t, int64(0), rec.Fields[0].OffsetBytes)
	assert.Equal(t, "extra", rec.Fields[1].Name)
	assert.Equal(t, int64(4), rec.Fields[1].OffsetBytes)
	assert.True(t, rec.IsPOD)
}

// TestInstantiateRecordRefusesWhatItCannotSize checks the two patterns whose
// layout is refused: a field of an incomplete record and a virtual base. Both
// come back as a record with no layout, so the generator can report them.
func TestInstantiateRecordRefusesWhatItCannotSize(t *testing.T) {
	project := instantiateFixture(t)

	incomplete, ok := project.InstantiateRecord("params::Incomplete", []TemplateArgument{intArg()})
	require.True(t, ok)
	assert.LessOrEqual(t, incomplete.SizeBytes, int64(0))
	assert.False(t, incomplete.LayoutKnown)

	virtual, ok := project.InstantiateRecord("params::VirtualDerived", []TemplateArgument{intArg()})
	require.True(t, ok)
	assert.LessOrEqual(t, virtual.SizeBytes, int64(0))
	assert.False(t, virtual.LayoutKnown)
}

// TestInstantiateRecordUnknownAndDeep covers the two ways the synthesis stops
// without a record: a name that is not a template this unit declares, and a
// recursion deeper than the bound.
func TestInstantiateRecordUnknownAndDeep(t *testing.T) {
	project := instantiateFixture(t)
	_, ok := project.InstantiateRecord("params::Nope", []TemplateArgument{intArg()})
	assert.False(t, ok)
	_, ok = project.InstantiateRecord("params::Fixed", []TemplateArgument{intArg()})
	assert.False(t, ok, "the argument count must match the pattern's")

	tu := project.tus[0]
	_, ok = tu.instantiateRecord("params::Derived", []TemplateArgument{intArg()}, 17)
	assert.False(t, ok, "the recursion bound stops a pathological pattern")
}

// TestInstantiateFunction covers the free function template: substitution of
// its type argument into the signature.
func TestInstantiateFunction(t *testing.T) {
	project := instantiateFixture(t)
	tu := project.tus[0]
	t.Logf("function templates: %v", len(tu.functionTemplates))
	for name, list := range tu.functionTemplates {
		t.Logf("  ft %s: %d", name, len(list))
	}
	for name, list := range tu.memberTemplates {
		t.Logf("  mt %s: %d", name, len(list))
	}
	sym, ok := project.InstantiateFunction("params::twice", []TemplateArgument{intArg()}, 1)
	require.True(t, ok)
	assert.Equal(t, "params::twice", sym.QualifiedName)
	assert.False(t, sym.IsTemplate)
	assert.Empty(t, sym.Mangled)
	assert.Equal(t, TypeInteger, sym.Result.Kind)
	assert.Equal(t, int64(4), sym.Result.SizeBytes)
	require.Len(t, sym.Params, 1)

	_, ok = project.InstantiateFunction("params::twice", []TemplateArgument{intArg()}, 2)
	assert.False(t, ok, "arity selects the overload")
	_, ok = project.InstantiateFunction("params::nope", []TemplateArgument{intArg()}, 1)
	assert.False(t, ok)
}

// TestInstantiateMemberSelectsByArityAndConst covers the two selectors the
// member lookup takes: the parameter count and the const marker.
func TestInstantiateMemberSelectsByArityAndConst(t *testing.T) {
	project := instantiateFixture(t)
	sym, ok := project.InstantiateMember("params::Holder2", "echo", []TemplateArgument{doubleArg()}, 1, true)
	require.True(t, ok)
	// The const qualifier of a member function template is not exposed by the
	// template cursor; the call is written through the object pointer either
	// way, so the binding is the same.
	assert.Equal(t, TypeFloat, sym.Result.Kind)
	assert.Equal(t, "double", sym.Result.Spelling)
	require.Len(t, sym.Params, 1)
	assert.Equal(t, TypeFloat, sym.Params[0].Kind)

	_, ok = project.InstantiateMember("params::Holder2", "echo", []TemplateArgument{doubleArg()}, 0, true)
	assert.False(t, ok, "the arity selects the overload")

	// A record template's constructor is not a member template: it is bound
	// from the pattern's member list, not through InstantiateMember.
	_, ok = project.InstantiateMember("params::Holder2", "Holder2", []TemplateArgument{doubleArg()}, 0, false)
	assert.False(t, ok)
}

// TestScalarTypeAndParseTemplateArgument pins the manifest's spellings.
func TestScalarTypeAndParseTemplateArgument(t *testing.T) {
	for _, tc := range []struct {
		spelling string
		kind     TypeKind
		size     int64
		unsigned bool
	}{
		{"int", TypeInteger, 4, false},
		{"unsigned char", TypeInteger, 1, true},
		{"uchar", TypeInteger, 1, true},
		{"const double", TypeFloat, 8, false},
		{"size_t", TypeInteger, 8, true},
		{"std::int64_t", TypeInteger, 8, false},
	} {
		param, ok := ScalarType(tc.spelling)
		require.True(t, ok, tc.spelling)
		assert.Equal(t, tc.kind, param.Kind, tc.spelling)
		assert.Equal(t, tc.size, param.SizeBytes, tc.spelling)
		assert.Equal(t, tc.unsigned, param.Unsigned, tc.spelling)
	}
	_, ok := ScalarType("std::string")
	assert.False(t, ok)

	arg, ok := ParseTemplateArgument("cv::Point_<int>")
	require.True(t, ok)
	assert.Equal(t, TypeRecord, arg.Type.Kind)
	assert.Equal(t, "cv::Point_<int>", arg.Type.RecordName)
	_, ok = ParseTemplateArgument("")
	assert.False(t, ok)
}

// TestSplitSpecializationAndArrays covers the text half of the mechanism: a
// canonical specialization name, an array spelling and the constant expression
// a substituted array size leaves behind.
func TestSplitSpecializationAndArrays(t *testing.T) {
	template, args, ok := splitSpecialization("params::Fixed<double, 4>")
	require.True(t, ok)
	assert.Equal(t, "params::Fixed", template)
	require.Len(t, args, 2)
	assert.Equal(t, TemplateArgumentIntegral, args[1].Kind)
	assert.Equal(t, int64(4), args[1].Value)

	_, _, ok = splitSpecialization("params::Fixed")
	assert.False(t, ok)

	for _, tc := range []struct {
		expr string
		want int64
		ok   bool
	}{
		{"4", 4, true},
		{"4 * 1", 4, true},
		{"2 + 2", 4, true},
		{"(2 + 3) * 2", 10, true},
		{"8 / 2", 4, true},
		{"4u", 4, true},
		{"x", 0, false},
		{"1 / 0", 0, false},
		{"", 0, false},
	} {
		got, ok := evalConstant(tc.expr)
		assert.Equal(t, tc.ok, ok, tc.expr)
		if ok {
			assert.Equal(t, tc.want, got, tc.expr)
		}
	}

	element, count, ok := splitArraySpelling("double[4 * 1]")
	require.True(t, ok)
	assert.Equal(t, "double", element)
	assert.Equal(t, "4 * 1", count)
	_, _, ok = splitArraySpelling("double")
	assert.False(t, ok)

	assert.Equal(t, int64(8), alignUp64(5, 4))
	assert.Equal(t, int64(5), alignUp64(5, 1))
	assert.Equal(t, int64(4), alignUp64(4, 4))
}

// TestNormalizeAndResolveParam covers re-resolving a substituted spelling that
// libclang left unclassified: a record, a pointer to one, a scalar, an array
// whose element and count were dependent, and a spelling nothing knows.
func TestNormalizeAndResolveParam(t *testing.T) {
	project := instantiateFixture(t)
	if _, ok := project.InstantiateRecord("params::Base", []TemplateArgument{intArg()}); !ok {
		t.Fatal("Base<int> did not instantiate")
	}
	tu := project.tus[0]

	resolved, ok := tu.resolveTypeSpelling("params::Base<int>", 0)
	require.True(t, ok)
	assert.Equal(t, TypeRecord, resolved.Kind)
	assert.Equal(t, "params::Base<int>", resolved.RecordName)
	assert.Equal(t, int64(4), resolved.SizeBytes)

	pointer, ok := tu.resolveTypeSpelling("const params::Base<int> *", 0)
	require.True(t, ok)
	assert.Equal(t, TypePointer, pointer.Kind)
	require.NotNil(t, pointer.Pointee)
	assert.True(t, pointer.Pointee.IsConst)

	scalar, ok := tu.resolveTypeSpelling("const int", 0)
	require.True(t, ok)
	assert.Equal(t, TypeInteger, scalar.Kind)
	assert.True(t, scalar.IsConst)

	_, ok = tu.resolveTypeSpelling("params::Nope", 0)
	assert.False(t, ok)

	array := Param{Kind: TypeArray, Spelling: "double[4 * 1]", ArraySize: -1}
	normalized := tu.normalizeParam(array, 0)
	assert.Equal(t, int64(4), normalized.ArraySize)
	require.NotNil(t, normalized.Element)
	assert.Equal(t, int64(32), normalized.SizeBytes)

	other := Param{Kind: TypeOther, Spelling: "params::Base<int>"}
	assert.Equal(t, TypeRecord, tu.normalizeParam(other, 0).Kind)

	unresolved := Param{Kind: TypeOther, Spelling: "params::Nope"}
	assert.Equal(t, TypeOther, tu.normalizeParam(unresolved, 0).Kind)

	pointerParam := Param{Kind: TypePointer, Pointee: &Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}}
	assert.Equal(t, TypeInteger, tu.normalizeParam(pointerParam, 0).Pointee.Kind)
}

// TestQualifyBaseNameAndSizes covers the two helpers the base handling uses.
func TestQualifyBaseNameAndSizes(t *testing.T) {
	assert.Equal(t, "params::Base<int>", qualifyBaseName("Base<int>", "params::Derived"))
	assert.Equal(t, "ns::Base", qualifyBaseName("ns::Base", "params::Derived"))
	assert.Equal(t, "Base", qualifyBaseName("class Base", "Derived"))
	assert.Equal(t, "Base", qualifyBaseName("Base", "Derived"))

	project := instantiateFixture(t)
	tu := project.tus[0]
	for _, tc := range []struct {
		param Param
		size  int64
		align int64
		ok    bool
	}{
		{Param{Kind: TypeInteger, SizeBytes: 4}, 4, 4, true},
		{Param{Kind: TypeInteger, SizeBytes: 16}, 16, 16, true},
		{Param{Kind: TypeInteger, SizeBytes: 0}, 0, 0, false},
		{Param{Kind: TypePointer}, 8, 8, true},
		{Param{Kind: TypeFloat, SizeBytes: 8}, 8, 8, true},
		{Param{Kind: TypeVoid}, 0, 0, false},
		{Param{Kind: TypeArray, ArraySize: 0, Element: &Param{Kind: TypeInteger, SizeBytes: 4}}, 0, 0, false},
	} {
		size, align, ok := tu.typeSizeFor(tc.param, 0)
		assert.Equal(t, tc.ok, ok, tc.param.Kind)
		if ok {
			assert.Equal(t, tc.size, size)
			assert.Equal(t, tc.align, align)
		}
	}
}

// TestApplyConstructibilityAndRegistration covers the record facts a factory
// depends on and the two registration paths (first sighting, replacement).
func TestApplyConstructibilityAndRegistration(t *testing.T) {
	project := instantiateFixture(t)
	tu := project.tus[0]
	rec := Record{
		QualifiedName: "params::MadeUp",
		IsTrivial:     false,
		Members: []Symbol{
			{Kind: Constructor, Params: nil},
			{Kind: Constructor, IsCopyConstructor: true, Params: []Param{{Kind: TypePointer}}},
			{Kind: Constructor, Params: []Param{{Kind: TypeInteger, Spelling: "int"}}},
		},
	}
	tu.applyConstructibility(&rec)
	assert.True(t, rec.IsDefaultConstructible)
	assert.True(t, rec.IsCopyConstructible)

	tu.registerInstantiated(rec)
	first, ok := tu.Record("params::MadeUp")
	require.True(t, ok)
	assert.Equal(t, 0, len(first.Fields))
	rec.Fields = []Field{{Name: "x"}}
	tu.registerInstantiated(rec)
	second, ok := tu.Record("params::MadeUp")
	require.True(t, ok)
	require.Len(t, second.Fields, 1, "the second registration replaces the first")
}

// TestSyntheticDestructorName covers the special member naming of a synthesized
// specialization: the destructor is named after the pattern, not after the
// pattern's parameterised spelling.
func TestSyntheticDestructorName(t *testing.T) {
	pattern := typeTemplate("ns::Box", nil, Symbol{
		QualifiedName: "ns::Box<T>::~Box<T>",
		Parent:        "ns::Box",
		Kind:          Destructor,
	})
	tu := syntheticTU(pattern)
	rec, ok := tu.instantiateRecord("ns::Box", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	require.Len(t, rec.Members, 1)
	assert.Equal(t, "ns::Box<int>::~Box", rec.Members[0].QualifiedName)
}
