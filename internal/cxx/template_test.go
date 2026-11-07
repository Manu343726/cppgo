package cxx

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExceptionSpecStrings(t *testing.T) {
	for _, tc := range []struct {
		spec     ExceptionSpec
		want     string
		canThrow bool
	}{
		{ExceptionUnspecified, "unspecified", true},
		{ExceptionNone, "throw()", false},
		{ExceptionDynamic, "dynamic", true},
		{ExceptionNoexcept, "noexcept", false},
		{ExceptionComputedNoexcept, "computed noexcept", true},
		{ExceptionNoThrow, "__declspec(nothrow)", false},
		{ExceptionUnknown, "unknown", true},
		{ExceptionSpec(99), "unknown", true},
	} {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.spec.String())
			assert.Equal(t, tc.canThrow, tc.spec.CanThrow())
		})
	}
}

func TestExceptionSpecFromHeader(t *testing.T) {
	symbols := headerSymbols(t)

	noexceptAdd := symbols["mathx::noexcept_add"]
	assert.Equal(t, ExceptionNoexcept, noexceptAdd.ExceptionSpec)
	assert.True(t, noexceptAdd.IsNoExcept)

	throwNone := symbols["mathx::throw_none_add"]
	assert.Equal(t, ExceptionNone, throwNone.ExceptionSpec)
	assert.True(t, throwNone.IsNoExcept, "throw() promises no exception")

	computed := symbols["mathx::computed_add"]
	assert.Equal(t, ExceptionComputedNoexcept, computed.ExceptionSpec)
	assert.False(t, computed.IsNoExcept, "a computed noexcept is conservatively allowed to throw")

	plain := symbols["mathx::add"]
	assert.Equal(t, ExceptionUnspecified, plain.ExceptionSpec)
	assert.False(t, plain.IsNoExcept)
}

func TestTemplateArgumentStrings(t *testing.T) {
	for _, tc := range []struct {
		arg  TemplateArgument
		want string
	}{
		{TemplateArgument{Kind: TemplateArgumentType, Spelling: "int"}, "int"},
		{TemplateArgument{Kind: TemplateArgumentIntegral, Spelling: "4"}, "4"},
		{TemplateArgument{Kind: TemplateArgumentPack}, "pack"},
		{TemplateArgument{Kind: TemplateArgumentTemplate}, "template"},
		{TemplateArgument{Kind: TemplateArgumentDeclaration}, "declaration"},
		{TemplateArgument{Kind: TemplateArgumentExpression}, "expression"},
		{TemplateArgument{Kind: TemplateArgumentUnknown}, "unknown"},
		{TemplateArgument{Kind: TemplateArgumentKind(99)}, "unknown"},
	} {
		assert.Equal(t, tc.want, tc.arg.String())
	}
}

func TestSpecializationName(t *testing.T) {
	typeArg := TemplateArgument{Kind: TemplateArgumentType, Spelling: "int"}
	assert.Equal(t, "mathx::Box<int>", specializationName("mathx::Box", []TemplateArgument{typeArg}))
	assert.Equal(t, "mathx::Fixed<double,4>", specializationName("mathx::Fixed", []TemplateArgument{
		{Kind: TemplateArgumentType, Spelling: "double"},
		{Kind: TemplateArgumentIntegral, Spelling: "4", Value: 4},
	}))
	assert.Equal(t, "mathx::Box<>", specializationName("mathx::Box", nil))
}

func TestTemplateArgumentsFromHeader(t *testing.T) {
	tu := recordFixture(t)

	pattern, ok := tu.Record("mathx::Box")
	require.True(t, ok)
	assert.True(t, pattern.IsTemplate)
	assert.False(t, pattern.IsTemplateSpecialization)
	assert.Empty(t, pattern.TemplateArguments)

	specialization, ok := tu.Record("mathx::Box<int>")
	require.True(t, ok, "an explicit instantiation is part of the model even though its cursor is not a definition")
	assert.True(t, specialization.IsTemplateSpecialization)
	assert.Equal(t, "mathx::Box", specialization.TemplateName)
	require.Len(t, specialization.TemplateArguments, 1)
	arg := specialization.TemplateArguments[0]
	assert.Equal(t, TemplateArgumentType, arg.Kind)
	assert.Equal(t, "int", arg.Spelling)
	assert.Equal(t, TypeInteger, arg.Type.Kind)
	assert.Equal(t, int64(4), arg.Type.SizeBytes)
	assert.False(t, specialization.LayoutKnown, "libclang defers the instantiated layout, so it is not mirrored")

	// An integral argument survives end to end: the field type's name carries
	// both arguments, which is what the generator names the Go type from.
	holder, ok := tu.Record("mathx::FixedHolder")
	require.True(t, ok)
	require.Len(t, holder.Fields, 1)
	assert.Equal(t, "mathx::Fixed<double,4>", holder.Fields[0].Type.RecordName)
}

func TestFunctionTemplatePattern(t *testing.T) {
	symbols := headerSymbols(t)

	maxOf := symbols["mathx::max_of"]
	assert.True(t, maxOf.IsTemplate, "libclang exposes the pattern, not the explicit instantiation, as a cursor")
	assert.Empty(t, maxOf.Mangled, "a pattern has no symbol of its own")
	require.Len(t, maxOf.Params, 2)
	assert.Equal(t, "T", maxOf.Params[0].Spelling)
	assert.Equal(t, "T", maxOf.Result.Spelling)
	assert.Equal(t, "mathx", maxOf.Parent)
}

func TestCursorIsValid(t *testing.T) {
	library := skipWithoutLibclang(t)
	assert.True(t, library.cursorIsValid(CXCursor{Kind: cursorClassDecl}))
	assert.False(t, library.cursorIsValid(CXCursor{Kind: cursorInvalidFile}))
	assert.False(t, library.cursorIsValid(CXCursor{Kind: cursorNoDeclFound}))
}

func TestPolymorphicHelper(t *testing.T) {
	library := skipWithoutLibclang(t)

	assert.False(t, library.polymorphic(CXCursor{Kind: cursorFunctionDecl}, 0), "a function is not a record")

	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })

	var vector CXCursor
	require.NoError(t, library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
		switch library.f.getCursorKind(cursor) {
		case cursorNamespace:
			return visitRecurse
		case cursorClassDecl:
			if library.str(library.f.getCursorSpelling(cursor)) == "Vector" {
				vector = cursor
				return visitBreak
			}
		}
		return visitContinue
	}))
	assert.True(t, library.polymorphic(vector, 0))
	assert.True(t, library.polymorphic(vector, maxHierarchyDepth+1), "out of budget assumes the worst")
}

func TestMergeIntoExceptionSpec(t *testing.T) {
	into := Symbol{QualifiedName: "mathx::noexcept_add"}
	mergeInto(&into, Symbol{QualifiedName: "mathx::noexcept_add", ExceptionSpec: ExceptionNoexcept})
	assert.Equal(t, ExceptionNoexcept, into.ExceptionSpec)
	assert.True(t, into.IsNoExcept)

	// A later sighting must not weaken a promise that was already made.
	mergeInto(&into, Symbol{QualifiedName: "mathx::noexcept_add"})
	assert.Equal(t, ExceptionNoexcept, into.ExceptionSpec)
	assert.True(t, into.IsNoExcept)
}
