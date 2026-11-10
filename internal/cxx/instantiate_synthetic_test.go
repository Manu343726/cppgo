package cxx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A synthetic translation unit is a TU whose extraction step has already run:
// the once is spent, and the record and symbol lists are whatever the test put
// there. The synthesis functions read those lists and the standard layout rules,
// not libclang, so their branches can be exercised without a fixture on disk —
// which is also why each of them is here rather than in a header.

// syntheticTU returns a TU with the extraction step already done.
func syntheticTU(records ...Record) *TU {
	tu := &TU{records: records, byRecord: make(map[string]int, len(records))}
	tu.once.Do(func() {})
	for i, rec := range records {
		tu.byRecord[rec.QualifiedName] = i
	}
	return tu
}

// intParam is the model's own `int`.
func intParam() Param {
	param, ok := ScalarType("int")
	if !ok {
		panic("int is not a scalar")
	}
	return param
}

// doubleParam is the model's own `double`.
func doubleParam() Param {
	param, ok := ScalarType("double")
	if !ok {
		panic("double is not a scalar")
	}
	return param
}

// intArgument and doubleArgument are manifest arguments.
func intArgument() TemplateArgument {
	return TemplateArgument{Kind: TemplateArgumentType, Spelling: "int", Type: intParam()}
}

func doubleArgument() TemplateArgument {
	return TemplateArgument{Kind: TemplateArgumentType, Spelling: "double", Type: doubleParam()}
}

// typeTemplate is a one-parameter class template pattern with the given fields
// and members.
func typeTemplate(name string, fields []Field, members ...Symbol) Record {
	return Record{
		QualifiedName:      name,
		Kind:               Struct,
		IsTemplate:         true,
		TemplateParameters: []TemplateParameter{{Name: "T", Kind: TemplateParameterType}},
		Fields:             fields,
		Members:            members,
		SizeBytes:          -1,
		AlignBytes:         -1,
	}
}

// TestSyntheticInstantiationGuards covers the ways the synthesis stops before it
// builds anything: the recursion bound, a name that is not a template, the wrong
// argument count, a type already being built, and a record this run already
// built.
func TestSyntheticInstantiationGuards(t *testing.T) {
	pattern := typeTemplate("ns::Box", []Field{{Name: "value", Type: Param{Kind: TypeOther, Spelling: "T"}, Access: AccessPublic, OffsetBytes: -1}})
	tu := syntheticTU(pattern)

	_, ok := tu.instantiateRecord("ns::Box", []TemplateArgument{intArgument()}, 17)
	assert.False(t, ok, "the recursion bound stops a pathological pattern")

	plain := Record{QualifiedName: "ns::Plain", Kind: Struct, SizeBytes: 4}
	tu = syntheticTU(pattern, plain)
	_, ok = tu.instantiateRecord("ns::Plain", []TemplateArgument{intArgument()}, 0)
	assert.False(t, ok, "a record that is not a template has no specialization")

	_, ok = tu.instantiateRecord("ns::Box", nil, 0)
	assert.False(t, ok, "the argument count has to match the pattern's parameters")

	// The synthesis map starts nil on a TU that has never built a record, and
	// that first build is what allocates it.
	require.Nil(t, tu.instantiating)
	rec, ok := tu.instantiateRecord("ns::Box", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	assert.Equal(t, "ns::Box<int>", rec.QualifiedName)

	// A second build of the same specialization returns the record the first one
	// registered instead of building it again.
	again, ok := tu.instantiateRecord("ns::Box", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	assert.Equal(t, rec.QualifiedName, again.QualifiedName)
	assert.Equal(t, rec.SizeBytes, again.SizeBytes)

	// A pattern already being built is the recursion the guard exists for: a
	// base or a field that mentions the type under construction.
	tu.instantiating["ns::Box<int>"] = true
	_, ok = tu.instantiateRecord("ns::Box", []TemplateArgument{intArgument()}, 0)
	assert.False(t, ok, "a specialization in the middle of its own build is not rebuilt")
	delete(tu.instantiating, "ns::Box<int>")

	// The project-level entry point publishes the record it built.
	project := &Project{tus: []*TU{tu}, records: []Record{}, byRecord: map[string]int{}}
	_, ok = project.InstantiateRecord("ns::Box", []TemplateArgument{doubleArgument()})
	require.True(t, ok)
	published, ok := project.Record("ns::Box<double>")
	require.True(t, ok)
	assert.Equal(t, int64(8), published.SizeBytes)
}

// TestSyntheticInstantiationMembers covers the member half: a pattern's own
// members are substituted, the pattern's member function templates are left out
// (a second parameter list the manifest would have to name), and the walk over
// the translation unit's symbol list finds them when the pattern record carries
// none.
func TestSyntheticInstantiationMembers(t *testing.T) {
	pattern := typeTemplate("ns::Box", []Field{{Name: "value", Type: intParam(), Access: AccessPublic, OffsetBytes: -1}},
		Symbol{
			QualifiedName: "ns::Box<T>::get",
			Parent:        "ns::Box",
			Kind:          Method,
			IsConst:       true,
			Result:        Param{Kind: TypeOther, Spelling: "T"},
			Params:        []Param{{Kind: TypeOther, Spelling: "T", Name: "fallback"}},
			Mangled:       "_ZN2ns3BoxIiE3getEv",
		},
	)
	tu := syntheticTU(pattern)
	rec, ok := tu.instantiateRecord("ns::Box", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	require.Len(t, rec.Members, 1)
	member := rec.Members[0]
	assert.Equal(t, "ns::Box<int>::get", member.QualifiedName)
	assert.Equal(t, "ns::Box<int>", member.Parent)
	assert.False(t, member.IsTemplate)
	assert.Empty(t, member.Mangled, "a synthesized member has no mangled symbol")
	assert.Equal(t, TypeInteger, member.Result.Kind, "the template argument substitutes into the result")
	require.Len(t, member.Params, 1)
	assert.Equal(t, TypeInteger, member.Params[0].Kind)
	assert.True(t, member.IsConst)

	// The pattern record carries no members of its own; the symbol walk's list
	// is where a class template's members live, and a member template of the
	// pattern is not something the specialization can expose.
	bare := typeTemplate("ns::Cell", nil)
	tu = syntheticTU(bare)
	tu.symbols = []Symbol{
		{QualifiedName: "ns::Cell<T>::set", Parent: "ns::Cell", Kind: Method, IsTemplate: true,
			Params: []Param{{Kind: TypeOther, Spelling: "T"}}},
		{QualifiedName: "ns::Cell<T>::convert", Parent: "ns::Cell", Kind: Method, IsTemplate: true,
			TemplateParameters: []TemplateParameter{{Name: "U", Kind: TemplateParameterType}}},
		{QualifiedName: "ns::Other::ignored", Parent: "ns::Other", Kind: Method, IsTemplate: true},
		{QualifiedName: "ns::Cell<T>::plain", Parent: "ns::Cell", Kind: Method, IsTemplate: false},
	}
	cell, ok := tu.instantiateRecord("ns::Cell", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	require.Len(t, cell.Members, 1, "only the pattern's own member is lowered")
	assert.Equal(t, "ns::Cell<int>::set", cell.Members[0].QualifiedName)
}

// TestSyntheticLayoutRefusals covers the layouts the walk refuses rather than
// guesses: a virtual base, a base whose size is unknown, and a field whose type
// the model cannot size.
func TestSyntheticLayoutRefusals(t *testing.T) {
	tu := syntheticTU()

	virtual := Record{QualifiedName: "ns::V", Bases: []Base{{QualifiedName: "ns::B", IsVirtual: true}}}
	assert.False(t, tu.layoutInstantiation(&virtual, 0), "a virtual base's offset is not a layout constant")

	unknown := Record{QualifiedName: "ns::U", Bases: []Base{{QualifiedName: "ns::Missing"}}}
	assert.False(t, tu.layoutInstantiation(&unknown, 0), "a base the model cannot size has no layout")

	incomplete := Record{QualifiedName: "ns::I", Fields: []Field{{Name: "f", Type: Param{Kind: TypeOther, Spelling: "ns::Missing"}, OffsetBytes: -1}}}
	assert.False(t, tu.layoutInstantiation(&incomplete, 0), "a field the model cannot size has no layout")
}

// TestSyntheticLayoutBaseFlattening covers the base handling that makes a
// mirrored Go struct the C layout: the base's fields are flattened with their
// offsets shifted, a private base field keeps the record out of the mirrored
// set, and a base that is not POD does too.
func TestSyntheticLayoutBaseFlattening(t *testing.T) {
	base := Record{
		QualifiedName: "ns::Base",
		Kind:          Struct,
		SizeBytes:     8,
		AlignBytes:    8,
		LayoutKnown:   true,
		IsPOD:         true,
		Fields: []Field{
			{Name: "value", Type: doubleParam(), Access: AccessPublic, OffsetBytes: 0},
			{Name: "hidden", Type: intParam(), Access: AccessPrivate, OffsetBytes: -1},
		},
	}
	tu := syntheticTU(base)

	rec := Record{
		QualifiedName: "ns::Derived",
		Bases:         []Base{{QualifiedName: "ns::Base"}},
		Fields:        []Field{{Name: "extra", Type: intParam(), Access: AccessPublic, OffsetBytes: -1}},
	}
	require.True(t, tu.layoutInstantiation(&rec, 0))
	assert.Equal(t, int64(0), rec.Bases[0].OffsetBytes)
	require.Len(t, rec.Fields, 2, "the public base field is flattened, the private one is not")
	assert.Equal(t, "value", rec.Fields[0].Name)
	assert.Equal(t, int64(0), rec.Fields[0].OffsetBytes)
	assert.Equal(t, "extra", rec.Fields[1].Name)
	assert.Equal(t, int64(8), rec.Fields[1].OffsetBytes)
	assert.Equal(t, int64(16), rec.SizeBytes)
	assert.False(t, rec.IsPOD, "a private base field keeps the layout out of the mirrored set")

	// A base that is not POD is flattened but marks the record non-POD.
	poly := Record{QualifiedName: "ns::Poly", Kind: Struct, SizeBytes: 8, AlignBytes: 8, LayoutKnown: true, IsPOD: false,
		Fields: []Field{{Name: "vptr", Type: Param{Kind: TypePointer}, Access: AccessPublic, OffsetBytes: 0}}}
	tu = syntheticTU(poly)
	derived := Record{QualifiedName: "ns::Widget", Bases: []Base{{QualifiedName: "ns::Poly"}}}
	require.True(t, tu.layoutInstantiation(&derived, 0))
	assert.False(t, derived.IsPOD)
	assert.False(t, derived.IsTrivial)
}

// TestSyntheticLayoutPodRules covers the field kinds that decide whether the
// layout can be a Go struct: an enum field is a named integer and still POD, a
// record field follows the record it names, and any other kind is not.
func TestSyntheticLayoutPodRules(t *testing.T) {
	plain := Record{QualifiedName: "ns::Inner", Kind: Struct, SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: true}
	foreign := Record{QualifiedName: "ns::Foreign", Kind: Struct, SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: false}

	tu := syntheticTU(plain, foreign)

	enums := Record{QualifiedName: "ns::WithEnum", Fields: []Field{
		{Name: "kind", Type: Param{Kind: TypeEnum, Spelling: "ns::Kind", SizeBytes: 4}, Access: AccessPublic, OffsetBytes: -1},
	}}
	require.True(t, tu.layoutInstantiation(&enums, 0))
	assert.True(t, enums.IsPOD, "an enum is a named integer")

	records := Record{QualifiedName: "ns::WithRecord", Fields: []Field{
		{Name: "inner", Type: Param{Kind: TypeRecord, Spelling: "ns::Inner", RecordName: "ns::Inner", SizeBytes: 4}, Access: AccessPublic, OffsetBytes: -1},
	}}
	require.True(t, tu.layoutInstantiation(&records, 0))
	assert.True(t, records.IsPOD, "a POD record field is POD")

	nonPod := Record{QualifiedName: "ns::WithForeign", Fields: []Field{
		{Name: "inner", Type: Param{Kind: TypeRecord, Spelling: "ns::Foreign", RecordName: "ns::Foreign", SizeBytes: 4}, Access: AccessPublic, OffsetBytes: -1},
	}}
	require.True(t, tu.layoutInstantiation(&nonPod, 0))
	assert.False(t, nonPod.IsPOD)
}

// TestSyntheticConstructibility covers the two character traits the generated
// factories depend on, including the deleted members that are not callable.
func TestSyntheticConstructibility(t *testing.T) {
	tu := syntheticTU()

	deleted := Record{QualifiedName: "ns::Deleted", Members: []Symbol{
		{Kind: Constructor, IsDeleted: true},
		{Kind: Constructor, IsCopyConstructor: true, IsDeleted: true},
		{Kind: Method},
	}}
	tu.applyConstructibility(&deleted)
	assert.False(t, deleted.IsDefaultConstructible, "a deleted default constructor is not callable")
	assert.False(t, deleted.IsCopyConstructible, "a deleted copy constructor is not callable")

	callable := Record{QualifiedName: "ns::Callable", Members: []Symbol{
		{Kind: Constructor},
		{Kind: Constructor, IsCopyConstructor: true},
	}}
	tu.applyConstructibility(&callable)
	assert.True(t, callable.IsDefaultConstructible)
	assert.True(t, callable.IsCopyConstructible)

	trivial := Record{QualifiedName: "ns::Trivial", IsTrivial: true}
	tu.applyConstructibility(&trivial)
	assert.True(t, trivial.IsCopyConstructible, "a trivial record is copyable without a declared copy constructor")
}

// TestSyntheticLayoutDestructorAndConstructors covers the member loop the layout
// uses to finish the record's traits.
func TestSyntheticLayoutDestructorAndConstructors(t *testing.T) {
	tu := syntheticTU()
	rec := Record{QualifiedName: "ns::Lifecycle", Fields: []Field{
		{Name: "value", Type: intParam(), Access: AccessPublic, OffsetBytes: -1},
	}, Members: []Symbol{
		{Kind: Constructor},
		{Kind: Constructor, IsCopyConstructor: true},
		{Kind: Destructor},
	}}
	require.True(t, tu.layoutInstantiation(&rec, 0))
	assert.Equal(t, int64(4), rec.SizeBytes)
	assert.False(t, rec.IsTrivial, "a declared destructor makes the record non-trivial")
	assert.False(t, rec.IsPOD)
	assert.True(t, rec.IsDefaultConstructible)
	assert.True(t, rec.IsCopyConstructible)
}

// TestSyntheticRegisterInstantiated covers the two registration paths: a new
// record, and a record that replaces the one already published under its name.
func TestSyntheticRegisterInstantiated(t *testing.T) {
	tu := syntheticTU()
	first := Record{QualifiedName: "ns::Thing", SizeBytes: 4}
	tu.registerInstantiated(first)
	published, ok := tu.Record("ns::Thing")
	require.True(t, ok)
	assert.Equal(t, int64(4), published.SizeBytes)

	second := first
	second.SizeBytes = 8
	tu.registerInstantiated(second)
	published, ok = tu.Record("ns::Thing")
	require.True(t, ok)
	assert.Equal(t, int64(8), published.SizeBytes)
	require.Len(t, tu.records, 1, "the record is replaced, not appended")
}

// TestSyntheticTypeSizes covers the arithmetic the layout depends on: the width
// and alignment of each kind, and the four ways a type has no size.
func TestSyntheticTypeSizes(t *testing.T) {
	tu := syntheticTU()
	for _, tc := range []struct {
		name  string
		param Param
		size  int64
		align int64
		ok    bool
	}{
		{"int", intParam(), 4, 4, true},
		{"double", doubleParam(), 8, 8, true},
		{"unsized integer", Param{Kind: TypeInteger}, 0, 0, false},
		{"pointer", Param{Kind: TypePointer}, 8, 8, true},
		{"array", Param{Kind: TypeArray, ArraySize: 3, Element: &Param{Kind: TypeInteger, SizeBytes: 4}}, 12, 4, true},
		{"array without an element", Param{Kind: TypeArray, ArraySize: 3}, 0, 0, false},
		{"empty array", Param{Kind: TypeArray, Element: &Param{Kind: TypeInteger, SizeBytes: 4}}, 0, 0, false},
		{"array of an unsized element", Param{Kind: TypeArray, ArraySize: 2, Element: &Param{Kind: TypeInteger}}, 0, 0, false},
		{"unknown record", Param{Kind: TypeRecord, RecordName: "ns::Missing"}, 0, 0, false},
		{"void", Param{Kind: TypeVoid, Spelling: "void"}, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			size, align, ok := tu.typeSizeFor(tc.param, 0)
			assert.Equal(t, tc.ok, ok)
			if ok {
				assert.Equal(t, tc.size, size)
				assert.Equal(t, tc.align, align)
			}
		})
	}
	assert.False(t, func() bool { _, _, ok := tu.typeSizeFor(intParam(), 17); return ok }(),
		"the recursion bound applies to a field's type as well")

	// A record field resolves through the record the model knows.
	tu = syntheticTU(Record{QualifiedName: "ns::Inner", SizeBytes: 12, AlignBytes: 4})
	size, align, ok := tu.typeSizeFor(Param{Kind: TypeRecord, RecordName: "ns::Inner"}, 0)
	require.True(t, ok)
	assert.Equal(t, int64(12), size)
	assert.Equal(t, int64(4), align)
}

// TestSyntheticBaseNameSubstitution covers the two shapes a base arrives in: a
// pattern records its spelling with the arguments ("Base<T>"), and a base with
// no spelling keeps its qualified name.
func TestSyntheticBaseNameSubstitution(t *testing.T) {
	base := Record{QualifiedName: "ns::Base", Kind: Struct, SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: true}
	pattern := typeTemplate("ns::Derived", nil)
	pattern.Bases = []Base{{QualifiedName: "ns::Base"}}
	tu := syntheticTU(base, pattern)

	rec, ok := tu.instantiateRecord("ns::Derived", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	require.Len(t, rec.Bases, 1)
	assert.Equal(t, "ns::Base", rec.Bases[0].QualifiedName, "a base with no spelling keeps its qualified name")

	// A spelling with the parameter substituted is qualified with the pattern's
	// namespace.
	spelled := typeTemplate("ns::Spelled", nil)
	spelled.Bases = []Base{{QualifiedName: "", Spelling: "Base<T>"}}
	tu = syntheticTU(base, spelled)
	rec, ok = tu.instantiateRecord("ns::Spelled", []TemplateArgument{intArgument()}, 0)
	require.True(t, ok)
	require.Len(t, rec.Bases, 1)
	assert.Equal(t, "ns::Base<int>", rec.Bases[0].QualifiedName)
}

// TestRecordBySpellingAmbiguity covers the lookup's three answers: the qualified
// name, the simple name when the spelling dropped it, and the ambiguity when two
// records share a simple name.
func TestRecordBySpellingAmbiguity(t *testing.T) {
	tu := syntheticTU(
		Record{QualifiedName: "a::Dup", SizeBytes: 4},
		Record{QualifiedName: "b::Dup", SizeBytes: 8},
		Record{QualifiedName: "ns::Unique", SizeBytes: 12},
	)
	found, ok := tu.recordBySpelling("ns::Unique")
	require.True(t, ok)
	assert.Equal(t, "ns::Unique", found.QualifiedName)

	found, ok = tu.recordBySpelling("Unique")
	require.True(t, ok)
	assert.Equal(t, "ns::Unique", found.QualifiedName)

	_, ok = tu.recordBySpelling("Dup")
	assert.False(t, ok, "two records with one simple name are not the same type")

	_, ok = tu.recordBySpelling("Nothing")
	assert.False(t, ok)
}

// TestSplitSpecializationSpellings covers the argument forms a specialization
// name can carry: an integral value, a scalar, and a record.
func TestSplitSpecializationSpellings(t *testing.T) {
	_, _, ok := splitSpecialization("ns::Fixed<double")
	assert.False(t, ok, "an unbalanced name is not a specialization")

	name, args, ok := splitSpecialization("ns::Fixed<double, 4, ns::Inner>")
	require.True(t, ok)
	assert.Equal(t, "ns::Fixed", name)
	require.Len(t, args, 3)
	assert.Equal(t, TemplateArgumentType, args[0].Kind)
	assert.Equal(t, "double", args[0].Spelling)
	assert.Equal(t, TemplateArgumentIntegral, args[1].Kind)
	assert.Equal(t, int64(4), args[1].Value)
	assert.Equal(t, TypeRecord, args[2].Type.Kind)
	assert.Equal(t, "ns::Inner", args[2].Type.RecordName)
}

// TestResolveTypeSpellingPrefixes covers the qualifiers a substituted spelling
// can still carry and the suffixes that make it a pointer.
func TestResolveTypeSpellingPrefixes(t *testing.T) {
	tu := syntheticTU(Record{QualifiedName: "ns::Thing", SizeBytes: 20, AlignBytes: 4})

	for _, spelling := range []string{"volatile int", "class ns::Thing", "struct ns::Thing"} {
		_, ok := tu.resolveTypeSpelling(spelling, 0)
		assert.True(t, ok, spelling)
	}
	pointer, ok := tu.resolveTypeSpelling("ns::Thing &&", 0)
	require.True(t, ok)
	assert.Equal(t, TypePointer, pointer.Kind)
	assert.Equal(t, "ns::Thing", pointer.Pointee.RecordName)

	_, ok = tu.resolveTypeSpelling("ns::Missing *", 0)
	assert.False(t, ok, "a pointer to a type the model does not know has no mapping")

	// A record with a plausible size that is not a specialization resolves
	// through the record list, not through the synthesis.
	resolved, ok := tu.resolveTypeSpelling("ns::Thing", 0)
	require.True(t, ok)
	assert.Equal(t, TypeRecord, resolved.Kind)
	assert.Equal(t, int64(20), resolved.SizeBytes)
}

// TestConstantExpressions covers the small evaluator a substituted array size
// needs, including the arithmetic a reader would not write but a substitution
// can produce.
func TestConstantExpressions(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want int64
		ok   bool
	}{
		{"4", 4, true},
		{"4 * 1", 4, true},
		{"2 + 2", 4, true},
		{"7 - 3", 4, true},
		{"(2 + 3) * 2", 10, true},
		{"8 / 2", 4, true},
		{"((3))", 3, true},
		{"4u", 4, true},
		{"4UL", 4, true},
		{"1 / 0", 0, false},
		{"(2 + 3", 0, false},
		{"()", 0, false},
		{"4+5", 9, true},
		{"x", 0, false},
		{"", 0, false},
		{"2 + ", 0, false},
	} {
		got, ok := evalConstant(tc.expr)
		assert.Equal(t, tc.ok, ok, tc.expr)
		if ok {
			assert.Equal(t, tc.want, got, tc.expr)
		}
	}
}

// TestSimpleMemberName covers the member name a specialization rewrites: the
// last component of a qualified name, and a name that has no qualifier at all.
func TestSimpleMemberName(t *testing.T) {
	assert.Equal(t, "get", simpleMemberName("ns::Box<int>::get"))
	assert.Equal(t, "~Box", simpleMemberName("ns::Box::~Box"))
	assert.Equal(t, "plain", simpleMemberName("plain"))
}

// TestParseTemplateArgumentEmpty pins the one spelling that is not an argument:
// the empty string.
func TestParseTemplateArgumentEmpty(t *testing.T) {
	_, ok := ParseTemplateArgument("")
	assert.False(t, ok)
	arg, ok := ParseTemplateArgument("  int  ")
	require.True(t, ok)
	assert.Equal(t, TemplateArgumentType, arg.Kind)
	assert.Equal(t, TypeInteger, arg.Type.Kind)
}

// TestNormalizeParamPointerAndOther covers the two shapes the substitution
// leaves for the second pass: a pointer whose pointee has to be re-resolved, and
// a type with no kind at all.
func TestNormalizeParamPointerAndOther(t *testing.T) {
	tu := syntheticTU(Record{QualifiedName: "ns::Thing", SizeBytes: 8, AlignBytes: 4})

	nilPointee := tu.normalizeParam(Param{Kind: TypePointer}, 0)
	assert.Nil(t, nilPointee.Pointee)

	pointer := tu.normalizeParam(Param{Kind: TypePointer, Pointee: &Param{Kind: TypeOther, Spelling: "ns::Thing"}}, 0)
	require.NotNil(t, pointer.Pointee)
	assert.Equal(t, TypeRecord, pointer.Pointee.Kind)

	// A spelling that is not an array keeps its shape.
	notArray := tu.normalizeParam(Param{Kind: TypeArray, Spelling: "ns::Thing", ArraySize: -1}, 0)
	assert.Equal(t, int64(-1), notArray.ArraySize)

	constOther := tu.normalizeParam(Param{Kind: TypeOther, Spelling: "ns::Thing", IsConst: true, Name: "value"}, 0)
	assert.Equal(t, TypeRecord, constOther.Kind)
	assert.True(t, constOther.IsConst)
	assert.Equal(t, "value", constOther.Name)
}

// TestMatchPartialRejectsTheWrongArity covers the first rule of partial
// specialization matching: a candidate whose argument count differs from the
// instantiation's is skipped, not scored.
func TestMatchPartialRejectsTheWrongArity(t *testing.T) {
	extractor := &recordExtractor{partials: map[string][]partialSpecialization{
		"ns::Box": {
			{args: []TemplateArgument{intArgument()}},
			{args: []TemplateArgument{intArgument(), intArgument()}},
		},
	}}
	_, _, ok := extractor.matchPartial("ns::Box", []TemplateArgument{intArgument(), intArgument(), intArgument()})
	assert.False(t, ok, "no candidate has that arity")
	_, _, ok = extractor.matchPartial("ns::Unknown", []TemplateArgument{intArgument()})
	assert.False(t, ok)
}

// TestMergeRecordPromotesTheTemplateSighting covers the merge of two sightings
// where only one of them knows the record is a template pattern.
func TestMergeRecordPromotesTheTemplateSighting(t *testing.T) {
	into := Record{QualifiedName: "ns::Box", SizeBytes: -1, AlignBytes: -1}
	from := Record{QualifiedName: "ns::Box", IsTemplate: true, SizeBytes: -1, AlignBytes: -1}
	mergeRecord(&into, from)
	assert.True(t, into.IsTemplate, "a sighting that saw the template marks the record as one")
}

// TestStaticMemberTemplateBindsAsAMethod covers what libclang can and cannot
// say about a member function template: its cursor is the template kind, so
// neither the method accessors nor the storage class answer, and the
// instantiation is a method. Calling a static member through an object is legal
// C++, so the call the shim writes is right either way.
func TestStaticMemberTemplateBindsAsAMethod(t *testing.T) {
	project := instantiateFixture(t)
	sym, ok := project.InstantiateMember("params::Holder2", "make", []TemplateArgument{doubleArgument()}, 1, false)
	require.True(t, ok)
	assert.Equal(t, Method, sym.Kind)
	assert.False(t, sym.IsStatic)
}
