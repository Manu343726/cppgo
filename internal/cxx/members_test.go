package cxx

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperatorMembers(t *testing.T) {
	symbols := headerSymbols(t)

	plus := symbols["mathx::Operators::operator+"]
	require.NotEmpty(t, plus.QualifiedName)
	assert.True(t, plus.IsOperator)
	assert.False(t, plus.IsConversion)
	assert.Equal(t, "+", plus.Operator)
	assert.Equal(t, Method, plus.Kind)
	assert.NotEmpty(t, plus.Mangled, "an out-of-line operator has a symbol")

	equal := symbols["mathx::Operators::operator=="]
	assert.True(t, equal.IsOperator)
	assert.Equal(t, "==", equal.Operator)

	at := symbols["mathx::Operators::operator[]"]
	assert.True(t, at.IsOperator)
	assert.Equal(t, "[]", at.Operator)

	call := symbols["mathx::Operators::operator int"]
	require.NotEmpty(t, call.QualifiedName)
	assert.True(t, call.IsOperator)
	assert.True(t, call.IsConversion, "a conversion operator's name is its target type")
	assert.Equal(t, "int", call.Operator)
	assert.Equal(t, TypeInteger, call.Result.Kind)
	assert.NotEmpty(t, call.Mangled)
}

func TestStaticDataMembers(t *testing.T) {
	tu := recordFixture(t)

	operators, ok := tu.Record("mathx::Operators")
	require.True(t, ok)
	require.Len(t, operators.StaticFields, 2)

	counter := operators.StaticFields[0]
	assert.Equal(t, "counter", counter.Name)
	assert.Equal(t, AccessPublic, counter.Access)
	assert.False(t, counter.IsConst, "a mutable static data member gets a setter")
	assert.Equal(t, TypeInteger, counter.Type.Kind)
	assert.NotEmpty(t, counter.Mangled, "the definition has a symbol")

	limit := operators.StaticFields[1]
	assert.Equal(t, "limit", limit.Name)
	assert.True(t, limit.IsConst, "a static const member has no setter")
	assert.Equal(t, int64(4), limit.Type.SizeBytes)

	box, ok := tu.Record("mathx::Box<int>")
	require.True(t, ok)
	require.Len(t, box.StaticFields, 1)
	shared := box.StaticFields[0]
	assert.Equal(t, "shared", shared.Name)
	assert.Equal(t, TypeInteger, shared.Type.Kind, "the template parameter is substituted")
	assert.Empty(t, shared.Mangled, "the pattern's mangling is not the instantiation's")
}

func TestNestedTypes(t *testing.T) {
	tu := recordFixture(t)

	entry, ok := tu.Record("mathx::Operators::Entry")
	require.True(t, ok, "a nested type of a plain record is its own qualified record")
	assert.True(t, entry.LayoutKnown)
	require.Len(t, entry.Fields, 1)
	assert.Equal(t, "index", entry.Fields[0].Name)

	inner, ok := tu.Record("mathx::Box<int>::Inner")
	require.True(t, ok, "a nested type of a specialisation is synthesised from the pattern")
	assert.True(t, inner.IsTemplateSpecialization == false, "the nested record is a type, not the template")
	require.Len(t, inner.Fields, 1)
	assert.Equal(t, "inner", inner.Fields[0].Name)
	assert.Equal(t, TypeInteger, inner.Fields[0].Type.Kind, "the outer argument is substituted")
	assert.False(t, inner.LayoutKnown, "the instantiated layout is deferred")
}

func TestMemberTemplateInstantiation(t *testing.T) {
	tu := recordFixture(t)

	box, ok := tu.Record("mathx::Box<int>")
	require.True(t, ok)
	require.Len(t, box.MemberTemplates, 1)
	pattern := box.MemberTemplates[0]
	assert.Equal(t, "mathx::Box<int>::convert", pattern.QualifiedName)
	assert.True(t, pattern.IsTemplate)
	require.Len(t, pattern.Params, 1)
	assert.Equal(t, "U", pattern.Params[0].Spelling)

	double := TemplateArgument{Kind: TemplateArgumentType, Spelling: "double",
		Type: Param{Kind: TypeFloat, Spelling: "double", SizeBytes: 8}}
	member, ok := tu.InstantiateMember("mathx::Box<int>", "convert", []TemplateArgument{double})
	require.True(t, ok)
	assert.Equal(t, "mathx::Box<int>::convert", member.QualifiedName)
	assert.False(t, member.IsTemplate, "the instantiation is a concrete member")
	assert.Empty(t, member.Mangled, "libclang exposes no symbol for it; the thunk carries the call")
	require.Len(t, member.Params, 1)
	assert.Equal(t, TypeFloat, member.Params[0].Kind)
	assert.Equal(t, int64(8), member.Params[0].SizeBytes)
	assert.Equal(t, "double", member.Result.Spelling)

	_, ok = tu.InstantiateMember("mathx::Box<int>", "convert", nil)
	assert.False(t, ok, "the argument count must match the template parameters")
	_, ok = tu.InstantiateMember("mathx::Box<int>", "does_not_exist", []TemplateArgument{double})
	assert.False(t, ok)
}

func TestOperatorInfo(t *testing.T) {
	for _, tc := range []struct {
		spelling   string
		result     Param
		operator   string
		conversion bool
	}{
		{"operator+", Param{}, "+", false},
		{"operator==", Param{}, "==", false},
		{"operator[]", Param{}, "[]", false},
		{"operator()", Param{}, "()", false},
		{"operator new", Param{Kind: TypePointer}, "new", false},
		{"operator int", Param{Kind: TypeInteger}, "int", true},
		{"operator std::string", Param{Kind: TypeRecord, Spelling: "std::string"}, "std::string", true},
		{"value", Param{Kind: TypeInteger}, "", false},
	} {
		isOperator, operator, conversion := operatorInfo(tc.spelling, tc.result)
		if tc.operator == "" {
			assert.False(t, isOperator, tc.spelling)
			continue
		}
		assert.True(t, isOperator, tc.spelling)
		assert.Equal(t, tc.operator, operator, tc.spelling)
		assert.Equal(t, tc.conversion, conversion, tc.spelling)
	}
}

func TestConstructibilityOfTrivialComposition(t *testing.T) {
	records := recordByName(t)

	hasPair, ok := records["mathx::HasPair"]
	require.True(t, ok)
	assert.True(t, hasPair.IsDefaultConstructible, "a nested trivial record and an array of it are trivially constructible")
	assert.True(t, hasPair.IsCopyConstructible)
}

func TestTrivialType(t *testing.T) {
	e := &recordExtractor{byName: map[string]int{}}

	assert.True(t, e.trivialType(Param{Kind: TypeInteger}, 0))
	assert.True(t, e.trivialType(Param{Kind: TypeArray, ArraySize: 2, Element: &Param{Kind: TypeFloat}}, 0))
	assert.False(t, e.trivialType(Param{Kind: TypeVoid}, 0))
	assert.False(t, e.trivialType(Param{Kind: TypeArray}, 0), "an array without an element is not vouched for")
	assert.False(t, e.trivialType(Param{Kind: TypeRecord, RecordName: "unknown"}, 0))

	deep := Param{Kind: TypeInteger}
	for i := 0; i < maxTrivialityDepth+1; i++ {
		deep = Param{Kind: TypeArray, Element: &deep}
	}
	assert.False(t, e.trivialType(deep, 0), "the recursion budget is honoured")
}

func TestArraySizeSubstitution(t *testing.T) {
	sub := substitution{names: map[string]TemplateArgument{
		"N": {Kind: TemplateArgumentIntegral, Value: 4},
		"T": {Kind: TemplateArgumentType, Spelling: "double"},
	}}
	assert.Equal(t, int64(4), sub.arraySize("T[N]"), "the bound is the integral argument")
	assert.Equal(t, int64(-1), sub.arraySize("double[4]"), "a literal bound has no parameter to resolve")
}

func TestOperatorAlone(t *testing.T) {
	isOperator, _, _ := operatorInfo("operator", Param{Kind: TypeVoid})
	assert.True(t, isOperator)
}

func TestProjectInstantiateMember(t *testing.T) {
	project := openFixture(t)
	member, ok := project.InstantiateMember("mathx::Box<int>", "convert", []TemplateArgument{
		{Kind: TemplateArgumentType, Spelling: "double", Type: Param{Kind: TypeFloat, Spelling: "double", SizeBytes: 8}},
	})
	require.True(t, ok)
	assert.Equal(t, "double", member.Result.Spelling)

	_, ok = project.InstantiateMember("mathx::DoesNotExist", "convert", nil)
	assert.False(t, ok)
}

func TestTemplateParameterIndex(t *testing.T) {
	depth, index, ok := templateParameterIndex("type-parameter-0-1")
	require.True(t, ok)
	assert.Equal(t, 0, depth)
	assert.Equal(t, 1, index)

	for _, spelling := range []string{"T", "type-parameter-x-0", "type-parameter-0"} {
		_, _, ok := templateParameterIndex(spelling)
		assert.False(t, ok, spelling)
	}
}

func TestSubstitutionArgumentFor(t *testing.T) {
	sub := substitution{
		names:   map[string]TemplateArgument{"T": {Kind: TemplateArgumentType, Spelling: "double"}},
		indexed: []TemplateArgument{{Kind: TemplateArgumentIntegral, Value: 4}},
	}
	byName, ok := sub.argumentFor("const T")
	require.True(t, ok)
	assert.Equal(t, TemplateArgumentType, byName.Kind)
	byIndex, ok := sub.argumentFor("type-parameter-0-0")
	require.True(t, ok)
	assert.Equal(t, TemplateArgumentIntegral, byIndex.Kind)
	_, ok = sub.argumentFor("type-parameter-1-0")
	assert.False(t, ok, "a member's own parameter depth is not the class's")
	_, ok = sub.argumentFor("unknown")
	assert.False(t, ok)
}

func TestConstructibilityThroughSpecializationFields(t *testing.T) {
	records := recordByName(t)

	boxHolder, ok := records["mathx::BoxHolder"]
	require.True(t, ok)
	boxDouble, ok := records["mathx::Box<double>"]
	require.True(t, ok)
	// libclang may complete this instantiation or defer it, depending on the
	// environment's standard headers; both are valid and both must satisfy the
	// layout contract.
	assertLayoutIsContractual(t, boxDouble)
	assert.True(t, boxDouble.IsTrivial)
	assert.True(t, boxDouble.IsDefaultConstructible)
	assert.True(t, boxHolder.IsDefaultConstructible, "a field the model classifies as constructible qualifies")
	assert.True(t, boxHolder.IsCopyConstructible)

	fixedHolder, ok := records["mathx::FixedHolder"]
	require.True(t, ok)
	assert.True(t, fixedHolder.IsDefaultConstructible)
	assert.True(t, fixedHolder.IsCopyConstructible)
}

func TestNestedEnums(t *testing.T) {
	tu := recordFixture(t)

	kind, ok := tu.Enum("mathx::Box<int>::Kind")
	require.True(t, ok, "a nested enum of a specialisation is synthesised from the pattern")
	assert.True(t, kind.IsScoped)
	assert.Equal(t, TypeInteger, kind.Underlying.Kind)
	assert.Equal(t, int64(4), kind.Underlying.SizeBytes)
	assert.True(t, kind.ConstantsKnown)
	require.Len(t, kind.Constants, 2)
	assert.Equal(t, "A", kind.Constants[0].Name)
	assert.Equal(t, int64(0), kind.Constants[0].Value)
	assert.Equal(t, "B", kind.Constants[1].Name)
	assert.Equal(t, int64(3), kind.Constants[1].Value, "the initializer is read from the pattern's tokens")

	level, ok := tu.Enum("mathx::Operators::Level")
	require.True(t, ok, "a nested enum of a plain record is a record of its own")
	assert.Equal(t, int64(5), level.Constants[1].Value)
	assert.True(t, level.Constants[1].IsUnsigned)
}

func TestStreamOperators(t *testing.T) {
	library := skipWithoutLibclang(t)
	header := fixturePath(t, "streams", "streams.hpp")
	tu, err := library.ParseTranslationUnit(header,
		[]string{"-std=c++17", "-I" + filepath.Dir(header), "-x", "c++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })
	symbols := make(map[string]Symbol)
	for _, symbol := range tu.Symbols() {
		symbols[symbol.QualifiedName] = symbol
	}

	write := symbols["streams::operator<<"]
	require.NotEmpty(t, write.QualifiedName)
	assert.True(t, write.IsOperator)
	assert.Equal(t, "<<", write.Operator)
	require.Len(t, write.Params, 2)
	assert.True(t, write.Params[0].IsStream(), "std::ostream resolves by its declaration spelling")
	assert.False(t, write.Params[1].IsStream())
	assert.True(t, write.Result.IsStream(), "the operator returns the stream by reference")

	read := symbols["streams::operator>>"]
	assert.True(t, read.Params[0].IsStream())
	assert.Equal(t, ">>", read.Operator)

	shift := symbols["streams::Shifter::operator<<"]
	assert.False(t, shift.Params[0].IsStream(), "an int shift is not a stream")
	assert.Equal(t, "<<", shift.Operator)
}

func TestNestedEnumInitializers(t *testing.T) {
	tu := recordFixture(t)

	kind, ok := tu.Enum("mathx::EnumBox<int,7>::Kind")
	require.True(t, ok)
	values := make(map[string]int64)
	for _, constant := range kind.Constants {
		values[constant.Name] = constant.Value
	}
	assert.Equal(t, int64(0), values["A"])
	assert.Equal(t, int64(3), values["B"])
	assert.Equal(t, int64(-2), values["C"])
	assert.Equal(t, int64(16), values["D"])
	assert.Equal(t, int64(7), values["E"], "a non-type template parameter is substituted")
	assert.False(t, kind.ConstantsKnown, "1 + 2 is neither a literal nor a parameter, so the model does not vouch")
}

func TestStreamClassification(t *testing.T) {
	for _, tc := range []struct {
		param Param
		want  bool
	}{
		{Param{Spelling: "std::ostream"}, true},
		{Param{Spelling: "std::ostream &"}, true},
		{Param{Spelling: "const std::ostream &"}, true},
		{Param{Spelling: "volatile std::istream &"}, true},
		{Param{Spelling: "std::wostream &"}, true},
		{Param{Spelling: "std::wistream &"}, true},
		{Param{RecordName: "std::basic_ostream<char,char_traits<char>>"}, true},
		{Param{RecordName: "std::basic_istream<char,char_traits<char>>"}, true},
		{Param{Spelling: "std::string"}, false},
		{Param{Spelling: "int"}, false},
		{Param{Ptr: true, Pointee: &Param{Spelling: "std::ostream"}}, true},
		{Param{Ptr: true, Pointee: &Param{Spelling: "int"}}, false},
	} {
		assert.Equal(t, tc.want, tc.param.IsStream(), tc.param.Spelling+"|"+tc.param.RecordName)
	}
}

func TestOffsetOfInvalidLocation(t *testing.T) {
	library := skipWithoutLibclang(t)
	e := &recordExtractor{l: library}
	path, offset, ok := e.offsetOf(CXSourceLocation{})
	assert.False(t, ok)
	assert.Empty(t, path)
	assert.Zero(t, offset)
}

func TestNestedOddDeclarations(t *testing.T) {
	tu := recordFixture(t)

	odd, ok := tu.Record("mathx::NestedOdd<int>")
	require.True(t, ok)
	require.Len(t, odd.Fields, 2)
	assert.Equal(t, "anonymous", odd.Fields[0].Name)
	assert.Equal(t, "pointer", odd.Fields[1].Name)

	// Unnamed nested records/enums have no name to bind and are dropped; the
	// forward declarations are not records yet.
	for _, record := range tu.Records() {
		assert.NotContains(t, record.QualifiedName, "unnamed")
	}
	_, ok = tu.Enum("mathx::NestedOdd<int>::FwdEnum")
	assert.False(t, ok, "an opaque enum declaration has no values to expose")
}

func TestBigEnumerationValues(t *testing.T) {
	tu := recordFixture(t)

	big, ok := tu.Enum("mathx::BigEnum<int>::Big")
	require.True(t, ok)
	assert.True(t, big.ConstantsKnown)
	require.Len(t, big.Constants, 1)
	assert.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), big.Constants[0].UnsignedValue)
	assert.Equal(t, int64(-1), big.Constants[0].Value, "the same bits read signed")
}

func TestSourceTextFailure(t *testing.T) {
	library := skipWithoutLibclang(t)
	e := &recordExtractor{l: library, sources: make(map[string][]byte)}
	_, ok := e.sourceText(CXCursor{})
	assert.False(t, ok)
	_, ok = e.enumeratorValue(CXCursor{}, substitution{}, 0)
	assert.False(t, ok)
}
