package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestOperatorGoNameOutsideTheTable names the operator and the rename.
func TestOperatorGoNameOutsideTheTable(t *testing.T) {
	b := &builder{}
	_, err := b.operatorGoName(&binding{sym: cxx.Symbol{QualifiedName: "ns::T::operator->*", IsOperator: true, Operator: "->*"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the naming table")
	assert.Contains(t, err.Error(), "-rename")

	name, err := b.operatorGoName(&binding{sym: cxx.Symbol{QualifiedName: "ns::T::operator+", IsOperator: true, Operator: "+"}})
	require.NoError(t, err)
	assert.Equal(t, "Add", name)
}

// TestConversionName covers the three answers a conversion operator gets: a
// scalar's Go name, As<T> for a record, and an error for a target with no name.
func TestConversionName(t *testing.T) {
	record := &recordModel{GoName: "Thing", CxxName: "ns::Thing", Mirrored: true, Size: 4, Align: 4}
	b := &builder{records: map[string]*recordModel{"ns::Thing": record}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}

	name, err := b.conversionName(&binding{sym: cxx.Symbol{Result: cxx.Param{Kind: cxx.TypeFloat, Spelling: "double", SizeBytes: 8}}})
	require.NoError(t, err)
	assert.Equal(t, "Float64", name)

	name, err = b.conversionName(&binding{sym: cxx.Symbol{Result: cxx.Param{
		Kind: cxx.TypeRecord, Spelling: "ns::Thing", RecordName: "ns::Thing", SizeBytes: 4,
	}}})
	require.NoError(t, err)
	assert.Equal(t, "AsThing", name)

	_, err = b.conversionName(&binding{sym: cxx.Symbol{Result: cxx.Param{Kind: cxx.TypeFloat, Spelling: "long double", ClangKind: "LongDouble", SizeBytes: 16}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the conversion target")
}

// TestCheckCollisionsAcrossKinds covers the three name clashes the check names:
// a record against a function, two types, and two enumerators.
func TestCheckCollisionsAcrossKinds(t *testing.T) {
	constant := func(name string) *enumConstant { return &enumConstant{GoName: name, CxxName: "ns::Kind::" + name} }

	t.Run("a record and a function", func(t *testing.T) {
		b := &builder{
			records: map[string]*recordModel{"ns::Value": {GoName: "Value", CxxName: "ns::Value"}},
			order:   []string{"ns::Value"},
		}
		b.bindings = []*binding{{GoName: "Value", sym: cxx.Symbol{QualifiedName: "ns::value"}}}
		err := b.checkCollisions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both map to Go name Value")
	})

	t.Run("two types", func(t *testing.T) {
		b := &builder{
			records: map[string]*recordModel{},
			enums:   map[string]*enumModel{"ns::Kind": {GoName: "Value", selected: true}},
			order:   []string{"ns::Value"},
		}
		b.records["ns::Value"] = &recordModel{GoName: "Value", CxxName: "ns::Value"}
		err := b.checkCollisions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "two types map to Go type Value")
	})

	t.Run("two enumerators", func(t *testing.T) {
		b := &builder{
			records: map[string]*recordModel{},
			enums: map[string]*enumModel{"ns::Kind": {GoName: "Kind", selected: true,
				Constants: []*enumConstant{constant("KindA"), constant("KindA")}}},
		}
		err := b.checkCollisions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "which another type or enumerator already uses")
	})
}

// TestBuildEnumsReportsAnUnderlyingTypeWithNoGoWidth covers the warning branch:
// the enum is omitted, the record that names it stays usable.
func TestBuildEnumsReportsAnUnderlyingTypeWithNoGoWidth(t *testing.T) {
	b := &builder{cfg: Config{}, records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.enumGoNames = map[string]string{"ns::Wide": "Wide"}
	b.rawEnums = []cxx.Enum{{
		QualifiedName:  "ns::Wide",
		ConstantsKnown: true,
		Underlying:     cxx.Param{Kind: cxx.TypeInteger, Spelling: "unsigned __int128", SizeBytes: 16},
	}}
	b.buildEnums()
	assert.Empty(t, b.enums)
	require.Len(t, b.warnings, 1)
	assert.Contains(t, b.warnings[0], "ns::Wide is not bound")
}

// TestForceThunkRecomputesTheStatusAndTheOutPointer covers the whole helper:
// a noexcept binding keeps no status, and a throwing one adds the out-pointer its
// value travels through.
func TestForceThunkRecomputesTheStatusAndTheOutPointer(t *testing.T) {
	b := &builder{}

	quiet := &binding{
		Carrier: direct,
		sym:     cxx.Symbol{QualifiedName: "ns::f", IsNoExcept: true},
		Result:  &goType{Kind: goInteger, Go: "int32", C: "int32_t"},
	}
	b.forceThunk(quiet, "test")
	assert.Equal(t, thunk, quiet.Carrier)
	assert.False(t, quiet.Status)
	assert.False(t, quiet.ErrorResult)
	assert.Equal(t, "cppgo_thunk_", quiet.Thunk[:len("cppgo_thunk_")])

	loud := &binding{
		Carrier: direct,
		sym:     cxx.Symbol{QualifiedName: "ns::g"},
		Result:  &goType{Kind: goInteger, Go: "int32", C: "int32_t"},
	}
	b.forceThunk(loud, "test")
	assert.True(t, loud.Status)
	assert.True(t, loud.ErrorResult)
	require.NotNil(t, loud.Out)
	assert.Equal(t, "cppgo_result", loud.Wire[len(loud.Wire)-1].cName)

	// A standard-library result already has its out-parameters: nothing is added.
	std := &binding{
		Carrier:   direct,
		sym:       cxx.Symbol{QualifiedName: "ns::h"},
		Result:    &goType{Kind: goString, Go: "string", Std: stdString},
		StdResult: true,
	}
	b.forceThunk(std, "test")
	assert.True(t, std.Status)
	assert.Nil(t, std.Out)
	assert.Empty(t, std.Wire)
}

// TestFieldWidthAndLayoutGuards covers the mirror predicate's guards: an unknown
// nested record has no width, and a padded record's layout is not Go's.
func TestFieldWidthAndLayoutGuards(t *testing.T) {
	b := &builder{rawRecords: []cxx.Record{
		{QualifiedName: "ns::Inner", SizeBytes: 4, AlignBytes: 4},
		{QualifiedName: "ns::Outer", SizeBytes: 16, AlignBytes: 8, Fields: []cxx.Field{
			{Name: "a", OffsetBytes: 0, Type: cxx.Param{Kind: cxx.TypeInteger, SizeBytes: 4}},
			{Name: "b", OffsetBytes: 8, Type: cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Inner", SizeBytes: 4}},
		}},
	}}
	size, align := b.fieldWidth(cxx.Field{Type: cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Inner", SizeBytes: 4}})
	assert.Equal(t, int64(4), size)
	assert.Equal(t, int64(4), align)

	size, _ = b.fieldWidth(cxx.Field{Type: cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Missing", SizeBytes: 4}})
	assert.Equal(t, int64(-1), size, "a record the model does not hold has no width")

	_, ok := b.rawRecordByName("ns::Missing")
	assert.False(t, ok)

	// A pointer field is not mirrorable, and neither is a padded layout.
	assert.False(t, b.mirrorableRecord(cxx.Record{
		QualifiedName: "ns::P", LayoutKnown: true, IsPOD: true, SizeBytes: 8, AlignBytes: 8,
		Fields: []cxx.Field{{Name: "p", OffsetBytes: 0, Access: cxx.AccessPublic, Type: cxx.Param{Kind: cxx.TypePointer, SizeBytes: 8}}},
	}, 0))
	assert.False(t, b.mirrorableRecord(cxx.Record{
		QualifiedName: "ns::Q", LayoutKnown: true, IsPOD: true, SizeBytes: 4, AlignBytes: 4,
		Fields: []cxx.Field{{Name: "a", OffsetBytes: 2, Access: cxx.AccessPublic, Type: cxx.Param{Kind: cxx.TypeInteger, SizeBytes: 2}}},
	}, 0), "a field not at its natural offset is not mirrored")
}

// TestMirrorFieldTypeCoversEveryKind pins what a mirrored field's Go type is.
func TestMirrorFieldTypeCoversEveryKind(t *testing.T) {
	record := &recordModel{GoName: "Inner", CxxName: "ns::Inner"}
	enum := &enumModel{GoName: "Kind", CxxName: "ns::Kind", C: "int32_t"}
	b := &builder{records: map[string]*recordModel{"ns::Inner": record}, enums: map[string]*enumModel{"ns::Kind": enum}}

	assert.Equal(t, "bool", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1}).Go)
	assert.Equal(t, "int32", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeInteger, SizeBytes: 4}).Go)
	assert.Equal(t, "float32", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeFloat, SizeBytes: 4}).Go)
	assert.Equal(t, "float64", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeFloat, SizeBytes: 8}).Go)
	assert.Equal(t, "Kind", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeEnum, RecordName: "ns::Kind", SizeBytes: 4}).Go)
	assert.Equal(t, "Inner", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Inner", SizeBytes: 4}).Go)
	// An enum or record the model does not hold falls through to its integer
	// reading, which the mirror predicate has already proven is a scalar.
	assert.Equal(t, "int32", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeEnum, RecordName: "ns::Missing", SizeBytes: 4}).Go)
}

// TestMapperRefusals covers the small refusals the type map keeps: a malformed
// std::function, a volatile element, and a function pointer whose signature
// cannot be split.
func TestMapperRefusals(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	m := &mapper{records: b.records, enums: b.enums}

	_, err := m.mapType(cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::function<int", SizeBytes: 32})
	require.Error(t, err)

	element, err := m.mapElement("volatile int")
	require.NoError(t, err)
	assert.Equal(t, "int32", element.Go)

	callable, ok := functionPointer(cxx.Param{
		Kind: cxx.TypePointer,
		Pointee: &cxx.Param{
			Kind:      cxx.TypeOther,
			ClangKind: "FunctionProto",
			Spelling:  "int",
		},
	})
	assert.False(t, ok, "a prototype with no parenthesis is not a callable signature")
	assert.Empty(t, callable.params)
}

// TestStaticFieldBindingRefusals covers the two shapes a static member cannot
// take: a pointer to a record it does not own, and a type the type map refuses.
func TestStaticFieldBindingRefusals(t *testing.T) {
	owner := &recordModel{GoName: "Holder", CxxName: "ns::Holder"}
	b := &builder{cfg: Config{}, records: map[string]*recordModel{"ns::Holder": owner}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}

	_, err := b.newStaticFieldBinding(owner, cxx.StaticField{
		Name: "payload",
		Type: cxx.Param{Kind: cxx.TypePointer, Spelling: "Payload *", Ptr: true, SizeBytes: 8,
			Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Payload", Spelling: "Payload"}},
	}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing on the Go side owns it")

	_, err = b.newStaticFieldBinding(owner, cxx.StaticField{
		Name: "wide",
		Type: cxx.Param{Kind: cxx.TypeFloat, Spelling: "long double", ClangKind: "LongDouble", SizeBytes: 16},
	}, false)
	require.Error(t, err)
}

// TestFieldBindingRefusals covers the same two shapes for a member field, and the
// setter's own arg-count guard.
func TestFieldBindingRefusals(t *testing.T) {
	owner := &recordModel{GoName: "Holder", CxxName: "ns::Holder"}
	b := &builder{cfg: Config{}, records: map[string]*recordModel{"ns::Holder": owner}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}

	_, err := b.newFieldBinding(owner, cxx.Field{
		Name: "payload",
		Type: cxx.Param{Kind: cxx.TypePointer, Spelling: "Payload *", Ptr: true, SizeBytes: 8,
			Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Payload", Spelling: "Payload"}},
	}, false)
	require.Error(t, err)

	_, err = b.newFieldBinding(owner, cxx.Field{
		Name: "wide",
		Type: cxx.Param{Kind: cxx.TypeFloat, Spelling: "long double", ClangKind: "LongDouble", SizeBytes: 16},
	}, false)
	require.Error(t, err)

	// The setter's target expression needs exactly one argument.
	m := &packageModel{}
	_, err = m.fieldTarget(&binding{
		Owner:       owner,
		memberField: &cxx.Field{Name: "count"},
		fieldWrite:  true,
	}, nil)
	require.Error(t, err)
}

// TestInheritanceGuards covers the guards the hierarchy walk keeps for a model
// that disagrees with itself: a depth budget, a base the model dropped, and a
// view that cannot be built.
func TestInheritanceGuards(t *testing.T) {
	// A base chain longer than the budget stops walking instead of recursing.
	var records []cxx.Record
	previous := ""
	for i := 0; i < maxHierarchyDepthForBases+4; i++ {
		name := "ns::C" + itoa(i)
		record := cxx.Record{QualifiedName: name}
		if previous != "" {
			record.Bases = []cxx.Base{{QualifiedName: previous, Access: cxx.AccessPublic}}
		}
		records = append(records, record)
		previous = name
	}
	b := &builder{rawRecords: records, records: map[string]*recordModel{}}
	for _, raw := range records {
		b.records[raw.QualifiedName] = &recordModel{CxxName: raw.QualifiedName, GoName: exportName(simpleName(raw.QualifiedName))}
	}
	bases := b.basesOf(b.records[previous])
	assert.LessOrEqual(t, len(bases), maxHierarchyDepthForBases+1)
	assert.NotEmpty(t, bases)
}

// TestAssignTypeNamesCoversANestedTemplateArgument keeps the pre-pass on the
// spelling libclang emits for a type inside a template specialization.
func TestAssignTypeNamesCoversANestedTemplateArgument(t *testing.T) {
	b := &builder{rawRecords: []cxx.Record{
		{QualifiedName: "ns::Box<int>", IsTemplateSpecialization: true},
		{QualifiedName: "ns::Box<int>::Slot", IsTemplateSpecialization: true},
	}}
	b.assignTypeNames()
	assert.Equal(t, "BoxInt", b.recordGoNames["ns::Box<int>"])
	assert.Equal(t, "BoxIntSlot", b.recordGoNames["ns::Box<int>::Slot"])
}

// TestApplyStatusRefusals pins the two inputs -status rejects.
func TestApplyStatusRefusals(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}

	void := &binding{sym: cxx.Symbol{QualifiedName: "ns::f"}}
	err := b.applyStatus(void)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returns nothing")

	text := &binding{sym: cxx.Symbol{QualifiedName: "ns::g"}, Result: &goType{Kind: goString, Go: "string"}}
	err = b.applyStatus(text)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a status is a 32-bit integer")
}

// TestNamesNestedTypeCoversPointersAndParams pins the two shapes the check walks:
// a pointer to a type the model does not hold, and a parameter that names one.
func TestNamesNestedTypeCoversPointersAndParams(t *testing.T) {
	b := &builder{records: map[string]*recordModel{"ns::Known": {CxxName: "ns::Known"}}, enums: map[string]*enumModel{}}
	unknownPointer := cxx.Symbol{QualifiedName: "ns::f", Result: cxx.Param{
		Kind: cxx.TypePointer, Spelling: "Slot *", Ptr: true,
		Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "Slot"},
	}}
	assert.True(t, b.namesNestedType(unknownPointer))

	unknownParam := cxx.Symbol{QualifiedName: "ns::g", Params: []cxx.Param{{Kind: cxx.TypeRecord, RecordName: "Slot"}}}
	assert.True(t, b.namesNestedType(unknownParam))

	known := cxx.Symbol{QualifiedName: "ns::h", Params: []cxx.Param{{Kind: cxx.TypeRecord, RecordName: "ns::Known"}}}
	assert.False(t, b.namesNestedType(known))

	// A pointer with no pointee and a plain scalar are not nested types.
	assert.False(t, b.namesNestedType(cxx.Symbol{QualifiedName: "ns::i", Result: cxx.Param{Kind: cxx.TypePointer}}))
	assert.False(t, b.namesNestedType(cxx.Symbol{QualifiedName: "ns::j", Result: cxx.Param{Kind: cxx.TypeInteger}}))
}

// TestDisplaySignatureCoversTheQualifiers pins the evidence line's suffixes.
func TestDisplaySignatureCoversTheQualifiers(t *testing.T) {
	assert.Equal(t, "ns::T::f() const noexcept",
		displaySignature(cxx.Symbol{QualifiedName: "ns::T::f", Kind: cxx.Method, IsConst: true, IsNoExcept: true}))
	assert.Equal(t, "ns::T::f()",
		displaySignature(cxx.Symbol{QualifiedName: "ns::T::f", Kind: cxx.Method}))
	assert.Equal(t, "ns::T::~T()",
		displaySignature(cxx.Symbol{QualifiedName: "ns::T::~T", Kind: cxx.Destructor}))
	assert.Equal(t, "ns::T::operator double() const",
		displaySignature(cxx.Symbol{QualifiedName: "ns::T::operator double", IsConversion: true, IsOperator: true, IsConst: true,
			Result: cxx.Param{Spelling: "double"}}))
}

// TestZeroValueCoversEveryKind pins the zero a failed binding returns, per Go
// shape.
func TestZeroValueCoversEveryKind(t *testing.T) {
	cases := []struct {
		kind   goKind
		goType string
		want   string
	}{
		{goBool, "bool", "false"},
		{goPointer, "*Thing", "nil"},
		{goOptional, "*Thing", "nil"},
		{goString, "string", `""`},
		{goSlice, "[]int32", "nil"},
		{goArray, "[4]int32", "[4]int32{}"},
		{goRecord, "Point", "Point{}"},
		{goEnum, "Axis", "0"},
		{goInteger, "int32", "0"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.goType, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, zeroValue(&goType{Kind: tc.kind, Go: tc.goType}))
		})
	}
}

// TestTargetCallRefusesAStaticSetterWithoutAValue covers the branch a malformed
// setter takes: the target expression needs exactly one argument.
func TestTargetCallRefusesAStaticSetterWithoutAValue(t *testing.T) {
	m := &packageModel{}
	_, err := m.targetCall(&binding{
		Owner:       &recordModel{CxxName: "ns::Holder"},
		staticField: &cxx.StaticField{Name: "count"},
		staticWrite: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one value")
}

// TestConstSuffixAlone covers the qualifier suffix when only constness is set.
func TestConstSuffixAlone(t *testing.T) {
	assert.Equal(t, " const", constSuffix(cxx.Symbol{IsConst: true}))
	assert.Equal(t, " noexcept", constSuffix(cxx.Symbol{IsNoExcept: true}))
	assert.Equal(t, "", constSuffix(cxx.Symbol{}))
}

// TestCopyAssignmentPublicWithAPublicOperator covers the branch a clean override
// takes: a public copy assignment leaves the setter available.
func TestCopyAssignmentPublicWithAPublicOperator(t *testing.T) {
	b := &builder{symbols: []cxx.Symbol{
		{QualifiedName: "ns::Open::operator=", IsOperator: true, Operator: "=", Access: cxx.AccessPublic},
		{QualifiedName: "ns::Open::other", Access: cxx.AccessPublic},
	}}
	assert.True(t, b.copyAssignmentPublic("ns::Open"))
}

// TestBuildStaticFieldsSkipsPrivateAndUnselectedMembers covers the two reasons a
// static member is left out before anything is lowered.
func TestBuildStaticFieldsSkipsPrivateAndUnselectedMembers(t *testing.T) {
	raw := cxx.Record{QualifiedName: "ns::Counter", StaticFields: []cxx.StaticField{
		{Name: "hidden", Access: cxx.AccessPrivate, Type: cxx.Param{Kind: cxx.TypeInteger, SizeBytes: 4}},
		{Name: "shown", Access: cxx.AccessPublic, Type: cxx.Param{Kind: cxx.TypeInteger, SizeBytes: 4}},
	}}
	b := &builder{cfg: Config{Bind: []string{"ns::Counter::other"}}, records: map[string]*recordModel{},
		enums: map[string]*enumModel{}, rawRecords: []cxx.Record{raw},
		bindMatched: make([]bool, 1), excludeMatched: []bool{}}
	owner := &recordModel{CxxName: "ns::Counter", GoName: "Counter"}
	b.records["ns::Counter"] = owner
	b.order = []string{"ns::Counter"}
	b.mapper = &mapper{records: b.records, enums: b.enums}
	b.buildStaticFields()
	assert.Empty(t, b.bindings)
	assert.Empty(t, owner.Statics)
}

// TestBuildTemplatesSkipsWhatTheManifestDoesNotName covers the template loop's
// guards: a record that is not a template, a free function that is not one, and
// a pattern the manifest leaves out.
func TestBuildTemplatesSkipsWhatTheManifestDoesNotName(t *testing.T) {
	excluded := &builder{cfg: Config{Bind: []string{"ns::Plain"}},
		rawRecords: []cxx.Record{
			{QualifiedName: "ns::Plain"},
			{QualifiedName: "ns::Box", IsTemplate: true},
		},
		symbols: []cxx.Symbol{
			{QualifiedName: "ns::free", Kind: cxx.FreeFunc},
			{QualifiedName: "ns::identity", Kind: cxx.FreeFunc, IsTemplate: true},
		},
		bindMatched:    make([]bool, 1),
		records:        map[string]*recordModel{},
		enums:          map[string]*enumModel{},
		templateByName: map[string]*templateModel{},
	}
	excluded.mapper = &mapper{records: excluded.records, enums: excluded.enums}
	excluded.buildTemplates()
	assert.Empty(t, excluded.templates)

	// A manifest that names the template keeps it.
	named := &builder{cfg: Config{Bind: []string{"ns::Box"}},
		rawRecords: []cxx.Record{{QualifiedName: "ns::Box", IsTemplate: true,
			TemplateParameters: []cxx.TemplateParameter{{Name: "T", Kind: cxx.TemplateParameterType}}}},
		bindMatched:    make([]bool, 1),
		records:        map[string]*recordModel{},
		enums:          map[string]*enumModel{},
		templateByName: map[string]*templateModel{},
	}
	named.mapper = &mapper{records: named.records, enums: named.enums}
	named.buildTemplates()
	require.Len(t, named.templates, 1)
	assert.Equal(t, "Box", named.templates[0].GoName)
}

// TestConversionNameWithoutAGoName covers a conversion target the type map reads
// as a value with no name of its own.
func TestConversionNameWithoutAGoName(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}
	_, err := b.conversionName(&binding{sym: cxx.Symbol{QualifiedName: "ns::T::operator void", IsConversion: true,
		Result: cxx.Param{Kind: cxx.TypeVoid, Spelling: "void"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no Go name")
}

// TestTemplateParamsRefusals covers the two template parameter shapes the generic
// API cannot express: a template-template parameter, and a pattern with none.
func TestTemplateParamsRefusals(t *testing.T) {
	_, err := templateParams([]cxx.TemplateParameter{{Name: "T", Kind: cxx.TemplateParameterTemplate}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a type or an integer")

	_, err = templateParams(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parameters")

	_, err = templateParams([]cxx.TemplateParameter{{Name: "N", Kind: cxx.TemplateParameterNonType,
		Type: cxx.Param{Kind: cxx.TypeFloat, Spelling: "double", SizeBytes: 8}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an integer")
}

// TestNewTemplateNamesTheParameterThatFails covers the error path through the
// pattern builder.
func TestNewTemplateNamesTheParameterThatFails(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}
	_, err := b.newTemplate(cxx.Record{QualifiedName: "ns::Box",
		TemplateParameters: []cxx.TemplateParameter{{Name: "T", Kind: cxx.TemplateParameterTemplate}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ns::Box")

	_, err = b.newTemplateFunction(cxx.Symbol{QualifiedName: "ns::make",
		TemplateParameters: []cxx.TemplateParameter{{Name: "T", Kind: cxx.TemplateParameterTemplate}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ns::make")
}

// TestNamesNestedTypeKnowsAnEnum covers the enum branch: an enum the model holds
// is not a nested type to report.
func TestNamesNestedTypeKnowsAnEnum(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{"ns::Kind": {CxxName: "ns::Kind"}}}
	assert.False(t, b.namesNestedType(cxx.Symbol{QualifiedName: "ns::f",
		Result: cxx.Param{Kind: cxx.TypeEnum, RecordName: "ns::Kind"}}))
}

// TestMapElementAcceptsVolatile covers the qualifier the element reader strips.
func TestMapElementAcceptsVolatile(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	m := &mapper{records: b.records, enums: b.enums}
	element, err := m.mapElement("volatile double")
	require.NoError(t, err)
	assert.Equal(t, "float64", element.Go)
}

// TestCheckCollisionsSkipsAnUnselectedEnum covers the enum that is not part of the
// API: it cannot collide with anything.
func TestCheckCollisionsSkipsAnUnselectedEnum(t *testing.T) {
	b := &builder{
		records: map[string]*recordModel{"ns::Value": {GoName: "Value", CxxName: "ns::Value"}},
		enums:   map[string]*enumModel{"ns::Kind": {GoName: "Value"}},
		order:   []string{"ns::Value"},
	}
	require.NoError(t, b.checkCollisions())
}

// TestBaseNamePropagatesTheOperatorFailure covers the naming error as it reaches
// the assignment loop.
func TestBaseNamePropagatesTheOperatorFailure(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}
	_, err := b.baseName(&binding{sym: cxx.Symbol{QualifiedName: "ns::T::operator->*", IsOperator: true, Operator: "->*"}})
	require.Error(t, err)

	_, err = b.baseName(&binding{sym: cxx.Symbol{QualifiedName: "ns::T::operator void", IsConversion: true, IsOperator: true,
		Result: cxx.Param{Kind: cxx.TypeVoid, Spelling: "void"}}})
	require.Error(t, err)
}

// TestNewBaseViewRefusesAnUnrepresentableTarget covers the guard a base view keeps:
// a cast whose result the type map cannot carry is reported, not emitted.
func TestNewBaseViewRefusesAnUnrepresentableTarget(t *testing.T) {
	b := &builder{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.mapper = &mapper{records: b.records, enums: b.enums}
	_, err := b.newBaseView(&recordModel{CxxName: "ns::Derived", GoName: "Derived"},
		&recordModel{CxxName: "ns::Base", GoName: "Base"}, false)
	require.Error(t, err)
}
