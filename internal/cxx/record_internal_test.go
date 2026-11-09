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
