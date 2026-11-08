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
