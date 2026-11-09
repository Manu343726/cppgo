package cxx

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchPartialArgs(t *testing.T) {
	bare := Param{Kind: TypeOther, ClangKind: "Unexposed", Spelling: "type-parameter-0-0"}
	pointer := Param{Kind: TypePointer, Ptr: true, Pointee: &bare}
	intArg := TemplateArgument{Kind: TemplateArgumentType, Spelling: "int", Type: Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}}
	intPtr := TemplateArgument{Kind: TemplateArgumentType, Spelling: "int *",
		Type: Param{Kind: TypePointer, Ptr: true, Spelling: "int *", Pointee: &Param{Kind: TypeInteger, Spelling: "int"}}}

	bound, score, ok := matchPartialArgs([]TemplateArgument{{Kind: TemplateArgumentType, Type: bare}}, []TemplateArgument{intArg})
	require.True(t, ok)
	assert.Equal(t, 0, score)
	assert.Equal(t, intArg.Spelling, bound[0].Spelling)

	bound, score, ok = matchPartialArgs([]TemplateArgument{{Kind: TemplateArgumentType, Type: pointer}}, []TemplateArgument{intPtr})
	require.True(t, ok)
	assert.Equal(t, 1, score)
	assert.Equal(t, "int", bound[0].Spelling, "the pointer pattern binds the pointee")

	_, _, ok = matchPartialArgs([]TemplateArgument{{Kind: TemplateArgumentType, Type: pointer}}, []TemplateArgument{intArg})
	assert.False(t, ok, "a pointer pattern does not match a non-pointer")

	concrete := TemplateArgument{Kind: TemplateArgumentType, Spelling: "int", Type: Param{Kind: TypeInteger, Spelling: "int"}}
	_, score, ok = matchPartialArgs([]TemplateArgument{concrete}, []TemplateArgument{intArg})
	require.True(t, ok)
	assert.Equal(t, 1, score)
	_, _, ok = matchPartialArgs([]TemplateArgument{concrete}, []TemplateArgument{{Kind: TemplateArgumentType, Spelling: "double", Type: Param{Kind: TypeFloat, Spelling: "double"}}})
	assert.False(t, ok, "a concrete component must equal the argument")
}

func TestAddRecordPrecedence(t *testing.T) {
	e := &recordExtractor{byName: make(map[string]int)}

	synthetic := Record{QualifiedName: "spec::Q<int>", Synthesized: true, SizeBytes: -1, AlignBytes: -1, Fields: []Field{{Name: "value"}}}
	e.addRecord(synthetic)
	require.Len(t, e.records, 1)

	// A real definition replaces a synthesized sighting.
	real := Record{QualifiedName: "spec::Q<int>", SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, Fields: []Field{{Name: "special"}}}
	e.addRecord(real)
	require.Len(t, e.records, 1)
	assert.False(t, e.records[0].Synthesized)
	assert.Equal(t, "special", e.records[0].Fields[0].Name)

	// A synthesized sighting does not replace a real one.
	e.addRecord(Record{QualifiedName: "spec::Q<int>", Synthesized: true, Fields: []Field{{Name: "value"}}})
	assert.Equal(t, "special", e.records[0].Fields[0].Name)
	assert.Equal(t, int64(4), e.records[0].SizeBytes)

	// Two real definitions merge.
	e.addRecord(Record{QualifiedName: "spec::Q<int>", Fields: []Field{{Name: "special"}}})
	require.Len(t, e.records, 1)

	// An unnamed record is dropped.
	e.addRecord(Record{})
	assert.Len(t, e.records, 1)
}

func TestCollectPartialRejectsANonTemplate(t *testing.T) {
	library := skipWithoutLibclang(t)
	e := &recordExtractor{
		l:        library,
		byName:   make(map[string]int),
		partials: make(map[string][]partialSpecialization),
	}
	e.collectPartial(CXCursor{})
	assert.Empty(t, e.partials)
}

func TestCursorDeclaresMembers(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })

	e := &recordExtractor{l: library}
	var full, instantiation CXCursor
	require.NoError(t, library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
		switch library.f.getCursorKind(cursor) {
		case cursorNamespace:
			return visitRecurse
		case cursorStructDecl:
			switch library.str(library.f.getCursorSpelling(cursor)) {
			case "Spec":
				full = cursor
			case "Box":
				instantiation = cursor
			}
		}
		return visitContinue
	}))
	require.True(t, library.cursorIsValid(full))
	require.True(t, library.cursorIsValid(instantiation))
	assert.True(t, e.cursorDeclaresMembers(full), "a full body exposes its fields and methods")
	// mathx.hpp's Box<int> is an explicit instantiation (no body of its own);
	// its cursor exposes no member cursors.
	assert.False(t, e.cursorDeclaresMembers(instantiation))
}

// TestCompleteLayout pins the derivation a synthesized record gets when
// libclang has no definition cursor for it. The cases are the ones the rule
// admits and the ones it refuses: a wrong size is worse than a reported unknown.
func TestCompleteLayout(t *testing.T) {
	intType := Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}
	doubleType := Param{Kind: TypeFloat, Spelling: "double", SizeBytes: 8}
	pointerType := Param{Kind: TypePointer, Spelling: "void *", Ptr: true, SizeBytes: 8}
	arrayType := Param{Kind: TypeArray, Spelling: "int[3]", SizeBytes: 12, Element: &intType}
	inner := Record{QualifiedName: "ns::Inner", SizeBytes: 16, AlignBytes: 8, LayoutKnown: true}
	enum := Enum{QualifiedName: "ns::Kind", Underlying: Param{Kind: TypeInteger, Spelling: "unsigned int", SizeBytes: 4}}
	unknown := Param{Kind: TypeOther, Spelling: "T", SizeBytes: -3}

	newExtractor := func() *recordExtractor {
		return &recordExtractor{
			byName: map[string]int{"ns::Inner": 0},
			records: []Record{
				inner,
			},
			byEnum: map[string]int{"ns::Kind": 0},
			enums:  []Enum{enum},
		}
	}

	t.Run("a plain record is laid out from its substituted fields", func(t *testing.T) {
		rec := Record{
			QualifiedName: "ns::Plain<int>",
			Synthesized:   true,
			Fields: []Field{
				{Name: "a", Type: intType},
				{Name: "b", Type: doubleType},
				{Name: "c", Type: pointerType},
			},
		}
		require.True(t, newExtractor().completeLayout(&rec))
		assert.Equal(t, int64(0), rec.Fields[0].OffsetBytes)
		assert.Equal(t, int64(8), rec.Fields[1].OffsetBytes, "a double after an int starts at its own alignment")
		assert.Equal(t, int64(16), rec.Fields[2].OffsetBytes)
		assert.Equal(t, int64(24), rec.SizeBytes)
		assert.Equal(t, int64(8), rec.AlignBytes)
	})

	t.Run("an array is the element's alignment", func(t *testing.T) {
		rec := Record{
			QualifiedName: "ns::Plain<int>",
			Synthesized:   true,
			Fields:        []Field{{Name: "a", Type: intType}, {Name: "b", Type: arrayType}},
		}
		require.True(t, newExtractor().completeLayout(&rec))
		assert.Equal(t, int64(4), rec.Fields[1].OffsetBytes)
		assert.Equal(t, int64(16), rec.SizeBytes)
	})

	t.Run("a nested record and an enum are the model's size and alignment", func(t *testing.T) {
		rec := Record{
			QualifiedName: "ns::Plain<int>",
			Synthesized:   true,
			Fields: []Field{
				{Name: "inner", Type: Param{Kind: TypeRecord, Spelling: "Inner", RecordName: "ns::Inner", SizeBytes: -1}},
				{Name: "kind", Type: Param{Kind: TypeEnum, Spelling: "Kind", RecordName: "ns::Kind", SizeBytes: -3}},
			},
		}
		require.True(t, newExtractor().completeLayout(&rec))
		assert.Equal(t, int64(16), rec.Fields[0].Type.SizeBytes, "the substituted nested record's size is the field's")
		assert.Equal(t, int64(4), rec.Fields[1].Type.SizeBytes, "the enum's underlying width is the field's")
		assert.Equal(t, int64(16), rec.Fields[1].OffsetBytes)
		assert.Equal(t, int64(24), rec.SizeBytes)
	})

	t.Run("a union holds every member at offset zero", func(t *testing.T) {
		rec := Record{
			QualifiedName: "ns::Plain<int>",
			Synthesized:   true,
			Kind:          Union,
			Fields:        []Field{{Name: "a", Type: intType}, {Name: "b", Type: doubleType}},
		}
		require.True(t, newExtractor().completeLayout(&rec))
		assert.Zero(t, rec.Fields[0].OffsetBytes)
		assert.Zero(t, rec.Fields[1].OffsetBytes)
		assert.Equal(t, int64(8), rec.SizeBytes)
		assert.Equal(t, int64(8), rec.AlignBytes)
	})

	t.Run("an empty record is one byte", func(t *testing.T) {
		rec := Record{QualifiedName: "ns::Plain<int>", Synthesized: true}
		require.True(t, newExtractor().completeLayout(&rec))
		assert.Equal(t, int64(1), rec.SizeBytes)
		assert.Equal(t, int64(1), rec.AlignBytes)
	})

	for _, tc := range []struct {
		name string
		rec  Record
		want string
	}{
		{
			name: "a base class",
			rec: Record{QualifiedName: "ns::Plain<int>", Synthesized: true,
				Bases: []Base{{QualifiedName: "ns::Base", OffsetBytes: 0}}},
			want: "the base's offset is not a field offset",
		},
		{
			name: "a virtual function",
			rec:  Record{QualifiedName: "ns::Plain<int>", Synthesized: true, HasVirtualFunction: true},
			want: "the vptr is part of the layout",
		},
		{
			name: "a bit-field",
			rec: Record{QualifiedName: "ns::Plain<int>", Synthesized: true,
				Fields: []Field{{Name: "bits", Type: intType, IsBitField: true}}},
			want: "a bit-field's allocation unit is not a field offset",
		},
		{
			name: "members of different access",
			rec: Record{QualifiedName: "ns::Plain<int>", Synthesized: true,
				Fields: []Field{{Name: "a", Type: intType, Access: AccessPublic}, {Name: "b", Type: intType, Access: AccessPrivate}}},
			want: "an implementation may reorder them",
		},
		{
			name: "a field with no size",
			rec: Record{QualifiedName: "ns::Plain<int>", Synthesized: true,
				Fields: []Field{{Name: "value", Type: unknown}}},
			want: "an unknown field size is not guessed",
		},
	} {
		tc := tc
		t.Run(tc.name+" is refused", func(t *testing.T) {
			assert.False(t, newExtractor().completeLayout(&tc.rec), tc.want)
			assert.False(t, tc.rec.LayoutKnown)
		})
	}
}

// TestDerivedPOD pins the POD answer a completed specialization gets, which is
// what lets Go mirror a plain nested record instead of treating it as opaque.
func TestDerivedPOD(t *testing.T) {
	rec := Record{
		QualifiedName: "ns::Plain<int>",
		Synthesized:   true,
		LayoutKnown:   true,
		Fields:        []Field{{Name: "value", Type: Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}, Access: AccessPublic}},
	}
	e := &recordExtractor{byName: map[string]int{}, records: []Record{}}
	assert.True(t, e.derivedPOD(&rec, ctorFacts{}), "one public int, no user-provided constructor or destructor")

	assert.False(t, e.derivedPOD(&rec, ctorFacts{userDeclared: true}), "a user-provided constructor makes it non-trivial")
	assert.False(t, e.derivedPOD(&rec, ctorFacts{userDestructor: true}), "a declared destructor makes it non-trivial")
	assert.False(t, e.derivedPOD(&rec, ctorFacts{copyCtor: true}), "a user-provided copy constructor makes it non-trivial")

	private := rec
	private.Fields = []Field{{Name: "value", Type: Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}, Access: AccessPrivate}}
	assert.False(t, e.derivedPOD(&private, ctorFacts{}), "a private member is not standard layout")

	base := rec
	base.Bases = []Base{{QualifiedName: "ns::Base"}}
	assert.False(t, e.derivedPOD(&base, ctorFacts{}), "a base class is not standard layout")
}

// TestCompleteLayoutRefusals covers the branches the fixture-driven tests
// cannot reach: the guard, the union's own refinement, and the alignment that
// no field type can answer.
func TestCompleteLayoutRefusals(t *testing.T) {
	intType := Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}
	unanswerable := Param{Kind: TypeOther, Spelling: "T", SizeBytes: 4}
	e := &recordExtractor{
		byName:  map[string]int{"ns::Dangling": 0},
		records: []Record{{QualifiedName: "ns::Dangling", SizeBytes: -1, AlignBytes: -1}},
		byEnum:  map[string]int{},
		enums:   nil,
	}

	known := Record{QualifiedName: "ns::Plain<int>", Synthesized: true, LayoutKnown: true}
	assert.False(t, e.completeLayout(&known), "a record whose layout is already known is not re-derived")

	unionBitfield := Record{QualifiedName: "ns::Plain<int>", Synthesized: true, Kind: Union,
		Fields: []Field{{Name: "bits", Type: intType, IsBitField: true}}}
	assert.False(t, e.completeLayout(&unionBitfield), "a union's bit-field has no field offset either")

	unionUnanswerable := Record{QualifiedName: "ns::Plain<int>", Synthesized: true, Kind: Union,
		Fields: []Field{{Name: "value", Type: unanswerable}}}
	assert.False(t, e.completeLayout(&unionUnanswerable), "a union member whose alignment is unknown refuses the derivation")

	emptyUnion := Record{QualifiedName: "ns::Plain<int>", Synthesized: true, Kind: Union}
	require.True(t, e.completeLayout(&emptyUnion))
	assert.Equal(t, int64(1), emptyUnion.SizeBytes, "an empty union is one byte, like an empty class")

	danglingMember := Record{QualifiedName: "ns::Plain<int>", Synthesized: true,
		Fields: []Field{{Name: "value", Type: Param{Kind: TypeRecord, Spelling: "Dangling", RecordName: "ns::Dangling", SizeBytes: 8}}}}
	assert.False(t, e.completeLayout(&danglingMember), "a member record whose alignment is unknown refuses the derivation")

	unanswerableField := Record{QualifiedName: "ns::Plain<int>", Synthesized: true,
		Fields: []Field{{Name: "value", Type: unanswerable}}}
	assert.False(t, e.completeLayout(&unanswerableField), "a field type with no alignment refuses the derivation")
}

// TestDerivedPODRefusesAnUnvouchedField covers the field the model cannot
// classify: a POD claim needs every member's type vouched for.
func TestDerivedPODRefusesAnUnvouchedField(t *testing.T) {
	rec := Record{
		QualifiedName: "ns::Plain<int>",
		Synthesized:   true,
		LayoutKnown:   true,
		Fields:        []Field{{Name: "value", Type: Param{Kind: TypeOther, Spelling: "T", SizeBytes: 4}, Access: AccessPublic}},
	}
	e := &recordExtractor{byName: map[string]int{}, records: []Record{}}
	assert.False(t, e.derivedPOD(&rec, ctorFacts{}), "a member the model cannot vouch for is not a POD")
}

// TestResolveNestedType pins the name the pattern spells against itself.
func TestResolveNestedType(t *testing.T) {
	e := &recordExtractor{
		byName:  map[string]int{"ns::Known": 0},
		records: []Record{{QualifiedName: "ns::Known"}},
		byEnum:  map[string]int{"ns::Kind": 0},
		enums:   []Enum{{QualifiedName: "ns::Kind"}},
	}
	specialization := Record{QualifiedName: "ns::Box<int>"}

	assert.Equal(t, "ns::Known", e.resolveNestedType(&specialization, "ns::Known"), "a record the model holds is its own name")
	assert.Equal(t, "ns::Kind", e.resolveNestedType(&specialization, "ns::Kind"), "an enum the model holds is its own name")
	assert.Equal(t, "", e.resolveNestedType(&specialization, ""), "an empty name stays empty")
	assert.Equal(t, "ns::Slot", e.resolveNestedType(&Record{QualifiedName: "ns::Plain"}, "ns::Slot"),
		"a plain record's nested name cannot be requalified without an instantiation")
	assert.Equal(t, "ns::Box<int>::Slot", e.resolveNestedType(&specialization, "ns::Box::Slot"),
		"a specialization's nested name is requalified against the instantiation")
	assert.Equal(t, "ns::Box<int>::Slot", e.resolveNestedType(&specialization, "Slot"),
		"a bare nested name is requalified too")
}

// TestAlignOf covers every type the derivation can be asked about, including
// the ones it cannot answer.
func TestAlignOf(t *testing.T) {
	e := &recordExtractor{
		byName: map[string]int{"ns::Laid": 0, "ns::Deferred": 1},
		records: []Record{
			{QualifiedName: "ns::Laid", SizeBytes: 16, AlignBytes: 8, LayoutKnown: true},
			{QualifiedName: "ns::Deferred", SizeBytes: -1, AlignBytes: -1},
		},
	}
	for _, tc := range []struct {
		name  string
		param Param
		want  int64
		ok    bool
	}{
		{"a pointer", Param{Kind: TypePointer, Ptr: true, SizeBytes: 8}, 8, true},
		{"an array", Param{Kind: TypeArray, SizeBytes: 12, Element: &Param{Kind: TypeInteger, SizeBytes: 4}}, 4, true},
		{"a laid-out record", Param{Kind: TypeRecord, RecordName: "ns::Laid", SizeBytes: 16}, 8, true},
		{"a record the model does not hold", Param{Kind: TypeRecord, RecordName: "ns::Missing"}, 0, false},
		{"a deferred record", Param{Kind: TypeRecord, RecordName: "ns::Deferred"}, 0, false},
		{"an enum", Param{Kind: TypeEnum, SizeBytes: 4}, 4, true},
		{"a scalar of no width", Param{Kind: TypeInteger, SizeBytes: 0}, 0, false},
		{"a type the model does not classify", Param{Kind: TypeOther, SizeBytes: 4}, 0, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := e.alignOf(tc.param)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRoundUp pins the alignment arithmetic.
func TestRoundUp(t *testing.T) {
	assert.Equal(t, int64(3), roundUp(3, 1), "an alignment of one rounds nothing")
	assert.Equal(t, int64(4), roundUp(3, 4))
	assert.Equal(t, int64(8), roundUp(8, 4))
	assert.Equal(t, int64(16), roundUp(9, 8))
}

// TestReconsiderLayout covers the retry itself: the derivation, the recorded
// raw size a plain record falls back to, and the two reasons to give up.
func TestReconsiderLayout(t *testing.T) {
	e := &recordExtractor{
		byName:  map[string]int{"ns::Inner": 0},
		records: []Record{{QualifiedName: "ns::Inner", SizeBytes: 8, AlignBytes: 8, LayoutKnown: true}},
	}
	intType := Param{Kind: TypeInteger, Spelling: "int", SizeBytes: 4}

	synthesized := Record{QualifiedName: "ns::Box<int>", Synthesized: true,
		Fields: []Field{{Name: "value", Type: intType}}}
	require.True(t, e.reconsiderLayout(&synthesized))
	assert.True(t, synthesized.LayoutKnown)
	assert.Equal(t, int64(4), synthesized.SizeBytes)

	// A record with a base class refuses the derivation (the base's offset is not
	// a field offset), so the raw size libclang gave is what is left.
	base := Base{QualifiedName: "ns::Base", OffsetBytes: 0}
	plain := Record{QualifiedName: "ns::Holder", rawSize: 16, rawAlign: 8, Bases: []Base{base},
		Fields: []Field{{Name: "inner", Type: Param{Kind: TypeRecord, RecordName: "ns::Inner", SizeBytes: 8}, OffsetBytes: 8}}}
	require.True(t, e.reconsiderLayout(&plain), "a record libclang laid out is trusted once its member types are known")
	assert.True(t, plain.LayoutKnown)
	assert.Equal(t, int64(16), plain.SizeBytes)
	assert.Equal(t, int64(8), plain.AlignBytes)

	noRaw := Record{QualifiedName: "ns::Holder", Bases: []Base{base},
		Fields: []Field{{Name: "value", Type: intType, OffsetBytes: 0}}}
	assert.False(t, e.reconsiderLayout(&noRaw), "no sizeof from libclang and no derivation leaves it unknown")

	noOffset := Record{QualifiedName: "ns::Holder", rawSize: 8, rawAlign: 8, Bases: []Base{base},
		Fields: []Field{{Name: "value", Type: intType, OffsetBytes: -1}}}
	assert.False(t, e.reconsiderLayout(&noOffset), "a field with no offset is not trusted even with a size")

	known := Record{QualifiedName: "ns::Holder", LayoutKnown: true}
	assert.False(t, e.reconsiderLayout(&known), "a known record is not retried")
}
