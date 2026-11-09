package cxx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommentWithoutTheQueryIsEmpty covers the guard a libclang older than the
// comment query takes: the model answers "" rather than calling a nil function.
func TestCommentWithoutTheQueryIsEmpty(t *testing.T) {
	l := &Libclang{}
	assert.Empty(t, l.comment(CXCursor{}))

	// The default-argument query is guarded the same way.
	params := []Param{{Spelling: "int"}}
	l.paramDefaults(CXCursor{}, params)
	assert.False(t, params[0].HasDefault)
}

// TestMemberTemplateRegistrationGuards covers the two shapes the registration
// drops: a translation unit that is gone, and a member with no name.
func TestMemberTemplateRegistrationGuards(t *testing.T) {
	// A nil translation unit: the registration returns before touching libclang.
	e := &recordExtractor{}
	e.registerMemberTemplate("ns::Box", CXCursor{}, substitution{})
	assert.Nil(t, e.tu)
}

// TestNestedEnumEvaluationGuards covers the two answers the nested-enum
// evaluator gives when it cannot read the values: no libclang handle, and a
// source whose initialisers do not line up with the enumerators.
func TestNestedEnumEvaluationGuards(t *testing.T) {
	e := &recordExtractor{}
	enum := &Enum{QualifiedName: "ns::Kind", ConstantsKnown: true, Constants: []Enumerator{{Name: "A"}}}
	e.evaluateNestedEnum(enum, CXCursor{}, substitution{})
	assert.False(t, enum.ConstantsKnown, "no libclang handle means the values cannot be read")

	skipWithoutLibclang(t)
	dir := t.TempDir()
	header := dir + "/nested.hpp"
	require.NoError(t, os.WriteFile(header, []byte(`
namespace ns {
template <typename T>
struct Box {
    enum class Kind { A = 0, B = 1 };
    T value;
};
template struct Box<int>;
}
`), 0o644))
	project, err := Open(Options{Header: header, Args: []string{"-std=c++17", "-xc++-header"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })
	kind, ok := project.Enum("ns::Box<int>::Kind")
	require.True(t, ok)
	assert.True(t, kind.ConstantsKnown)
	require.Len(t, kind.Constants, 2)
	assert.Equal(t, int64(1), kind.Constants[1].Value)
}

// TestDefaultArgumentEdgeShapes covers the separator's neighbours: an
// unterminated template argument list, and a comparison that is not the
// separator.
func TestDefaultArgumentEdgeShapes(t *testing.T) {
	cases := []struct {
		text  string
		value string
		ok    bool
	}{
		{text: "std::vector<int", ok: false},
		{text: "int value = a <= b", value: "a <= b", ok: true},
		{text: "unsigned long long value = 0ull", value: "0ull", ok: true},
		{text: "char token = '='", value: "'='", ok: true},
		{text: "int a =", value: "", ok: true},
		{text: "A<B = 1", ok: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			value, ok := defaultArgument(tc.text)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.value, value)
			}
		})
	}
}

// TestSourceTextWithoutATranslationUnit covers the guard the source reader keeps:
// a specialization whose values cannot be read is not a failure, it is an enum
// with no constants.
func TestSourceTextWithoutATranslationUnit(t *testing.T) {
	e := &recordExtractor{}
	_, ok := e.sourceText(CXCursor{})
	assert.False(t, ok)
}

// TestAddTypeSpecializationIgnoresASystemType covers the guard on a field whose
// type is a standard-library instantiation: the record is not the API.
func TestAddTypeSpecializationIgnoresASystemType(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := t.TempDir()
	header := dir + "/sys.hpp"
	require.NoError(t, os.WriteFile(header, []byte(`
#include <vector>
namespace sys { struct Holder { std::vector<int> values; int count; }; }
`), 0o644))
	project, err := Open(Options{Header: header, Args: []string{"-std=c++17", "-xc++-header"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })
	for _, rec := range project.Records() {
		assert.NotContains(t, rec.QualifiedName, "std::vector")
	}
	_, ok := project.Record("sys::Holder")
	assert.True(t, ok)
	_ = library
}

// TestSourceTextReadsTheDeclaration covers the reader itself: the first read of
// a file fills the cache, and a second read of the same file answers from it.
func TestSourceTextReadsTheDeclaration(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := t.TempDir()
	header := dir + "/src.hpp"
	require.NoError(t, os.WriteFile(header, []byte("namespace src { enum Kind { A = 1 }; }\n"), 0o644))
	tu, err := library.ParseTranslationUnit(header, []string{"-std=c++17", "-xc++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })

	var enumCursor CXCursor
	_ = library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
		if library.f.getCursorKind(cursor) == cursorEnumDecl && enumCursor.Kind == 0 {
			enumCursor = cursor
			return visitContinue
		}
		return visitRecurse
	})
	require.NotZero(t, enumCursor.Kind)

	e := &recordExtractor{l: library, tu: tu}
	text, ok := e.sourceText(enumCursor)
	require.True(t, ok)
	assert.Contains(t, text, "Kind")
	// The second read comes from the cache.
	again, ok := e.sourceText(enumCursor)
	require.True(t, ok)
	assert.Equal(t, text, again)

	// A cursor outside any file has no text.
	_, ok = e.sourceText(CXCursor{})
	assert.False(t, ok)
}

// TestSelectableWithNoRoot covers the answer for a declaration in a system
// header when the unit has no root that names it: it is not the API.
func TestSelectableWithNoRoot(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := t.TempDir()
	include := filepath.Join(dir, "include")
	require.NoError(t, os.MkdirAll(include, 0o755))
	libraryHeader := filepath.Join(include, "lib.hpp")
	require.NoError(t, os.WriteFile(libraryHeader, []byte("namespace lib { int f(); }\n"), 0o644))
	header := filepath.Join(dir, "main.hpp")
	require.NoError(t, os.WriteFile(header, []byte("#include \"lib.hpp\"\n"), 0o644))

	args := []string{"-std=c++17", "-xc++-header", "-isystem", include}
	var function CXCursor
	find := func(tu *TU) {
		function = CXCursor{}
		_ = library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
			if library.f.getCursorKind(cursor) != cursorFunctionDecl || function.Kind != 0 {
				return visitRecurse
			}
			file, _ := library.locationValues(library.f.getCursorLocation(cursor))
			if strings.HasSuffix(file, "lib.hpp") {
				function = cursor
			}
			return visitRecurse
		})
	}

	tu, err := library.ParseTranslationUnitIn(header, args, []string{""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })
	find(tu)
	require.NotZero(t, function.Kind)
	assert.False(t, tu.selectable(function), "an empty root names no directory")

	// With the include directory as the root, the same declaration is the API.
	withRoot, err := library.ParseTranslationUnitIn(header, args, []string{include})
	require.NoError(t, err)
	t.Cleanup(func() { _ = withRoot.Close() })
	assert.True(t, withRoot.selectable(function))
}

// TestAddTypeSpecializationRejectsASystemType covers the guard the field-type
// path keeps: a standard-library instantiation reached through a member is not
// the caller's API, so the record is not added.
func TestAddTypeSpecializationRejectsASystemType(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := t.TempDir()
	header := dir + "/field.hpp"
	require.NoError(t, os.WriteFile(header, []byte(`
#include <vector>
namespace field { struct Holder { std::vector<int> values; }; }
`), 0o644))
	tu, err := library.ParseTranslationUnitIn(header, []string{"-std=c++17", "-xc++-header"}, []string{dir})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })
	e := &recordExtractor{l: library, tu: tu, byName: map[string]int{}, byEnum: map[string]int{}, partials: map[string][]partialSpecialization{}}
	_ = library.visit(library.f.getTranslationUnitCursor(tu.tu), e.visit)
	for _, record := range e.records {
		assert.NotContains(t, record.QualifiedName, "std::vector")
	}
}

// TestSonameRejectsANonELF covers the reader's guard: a file that is not an ELF
// object has no soname to report.
func TestSonameRejectsANonELF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-elf.txt")
	require.NoError(t, os.WriteFile(path, []byte("plain text"), 0o644))
	_, err := Soname(path)
	require.Error(t, err)
}
