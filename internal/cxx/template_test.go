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
		kind TemplateArgumentKind
		want string
	}{
		{TemplateArgumentType, "type"},
		{TemplateArgumentIntegral, "integral"},
		{TemplateArgumentDeclaration, "declaration"},
		{TemplateArgumentTemplate, "template"},
		{TemplateArgumentExpression, "expression"},
		{TemplateArgumentPack, "pack"},
		{TemplateArgumentUnknown, "unknown"},
		{TemplateArgumentKind(99), "unknown"},
	} {
		assert.Equal(t, tc.want, tc.kind.String())
	}
	assert.Equal(t, "int", TemplateArgument{Kind: TemplateArgumentType, Spelling: "int"}.String())
	assert.Equal(t, "pack", TemplateArgument{Kind: TemplateArgumentPack}.String())
}

func TestExceptionSpecFromKind(t *testing.T) {
	for _, tc := range []struct {
		kind int32
		want ExceptionSpec
	}{
		{exceptionSpecNone, ExceptionUnspecified},
		{exceptionSpecDynamicNone, ExceptionNone},
		{exceptionSpecDynamic, ExceptionDynamic},
		{exceptionSpecMSAny, ExceptionDynamic},
		{exceptionSpecBasicNoexcept, ExceptionNoexcept},
		{exceptionSpecComputedNoexcept, ExceptionComputedNoexcept},
		{exceptionSpecUnevaluated, ExceptionUnknown},
		{exceptionSpecUninstantiated, ExceptionUnknown},
		{exceptionSpecUnparsed, ExceptionUnknown},
		{exceptionSpecNoThrow, ExceptionNoThrow},
		{-1, ExceptionUnknown},
		{99, ExceptionUnknown},
	} {
		assert.Equal(t, tc.want, exceptionSpecFromKind(tc.kind), "kind %d", tc.kind)
	}
}

func TestTemplateArgumentKindFromLibclang(t *testing.T) {
	for _, tc := range []struct {
		kind int32
		want TemplateArgumentKind
	}{
		{templateArgumentNull, TemplateArgumentUnknown},
		{templateArgumentType, TemplateArgumentType},
		{templateArgumentDeclaration, TemplateArgumentDeclaration},
		{templateArgumentNullPtr, TemplateArgumentUnknown},
		{templateArgumentIntegral, TemplateArgumentIntegral},
		{templateArgumentTemplate, TemplateArgumentTemplate},
		{templateArgumentExpansion, TemplateArgumentPack},
		{templateArgumentExpression, TemplateArgumentExpression},
		{templateArgumentPack, TemplateArgumentPack},
		{templateArgumentInvalid, TemplateArgumentUnknown},
		{-1, TemplateArgumentUnknown},
	} {
		assert.Equal(t, tc.want, TemplateArgumentKindFromLibclang(tc.kind), "kind %d", tc.kind)
	}
}

func TestInnerTemplateArguments(t *testing.T) {
	for _, tc := range []struct {
		spelling string
		want     []string
	}{
		{"Box<int>", []string{"int"}},
		{"mathx::Fixed<double, 4>", []string{"double", "4"}},
		{"Pair<Box<int>, double>", []string{"Box<int>", "double"}},
		{"Box", nil},
		{"Box<>", []string{""}},
	} {
		assert.Equal(t, tc.want, innerTemplateArguments(tc.spelling), tc.spelling)
	}
}

func TestTemplateArgumentsOnNonTemplate(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })

	var free CXCursor
	require.NoError(t, library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
		if library.f.getCursorKind(cursor) == cursorFunctionDecl &&
			library.str(library.f.getCursorSpelling(cursor)) == "add" {
			free = cursor
			return visitBreak
		}
		if library.f.getCursorKind(cursor) == cursorNamespace {
			return visitRecurse
		}
		return visitContinue
	}))
	require.True(t, library.cursorIsValid(free))
	assert.Empty(t, library.templateArguments(free), "a non-template declaration has no arguments")
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
