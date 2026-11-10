package cxx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReportVocabulary pins the strings every report, diagnostic and generated
// name is built from. These are the words a reader of the coverage report sees,
// so each kind libclang can hand back has a name and an unknown one is named as
// unknown rather than left blank.
func TestReportVocabulary(t *testing.T) {
	t.Run("template argument kind", func(t *testing.T) {
		for kind, want := range map[int32]TemplateArgumentKind{
			templateArgumentType:        TemplateArgumentType,
			templateArgumentIntegral:    TemplateArgumentIntegral,
			templateArgumentDeclaration: TemplateArgumentDeclaration,
			templateArgumentTemplate:    TemplateArgumentTemplate,
			templateArgumentExpression:  TemplateArgumentExpression,
			templateArgumentPack:        TemplateArgumentPack,
			templateArgumentExpansion:   TemplateArgumentPack,
			templateArgumentNull:        TemplateArgumentUnknown,
			templateArgumentNullPtr:     TemplateArgumentUnknown,
			templateArgumentInvalid:     TemplateArgumentUnknown,
			-1:                          TemplateArgumentUnknown,
		} {
			assert.Equal(t, want, TemplateArgumentKindFromLibclang(kind), "libclang kind %d", kind)
		}
	})

	t.Run("template argument kind names", func(t *testing.T) {
		for kind, want := range map[TemplateArgumentKind]string{
			TemplateArgumentType:        "type",
			TemplateArgumentIntegral:    "integral",
			TemplateArgumentDeclaration: "declaration",
			TemplateArgumentTemplate:    "template",
			TemplateArgumentExpression:  "expression",
			TemplateArgumentPack:        "pack",
			TemplateArgumentUnknown:     "unknown",
			TemplateArgumentKind(200):   "unknown",
		} {
			assert.Equal(t, want, kind.String())
		}
	})

	t.Run("template parameter kind names", func(t *testing.T) {
		for kind, want := range map[TemplateParameterKind]string{
			TemplateParameterType:      "type",
			TemplateParameterNonType:   "non-type",
			TemplateParameterTemplate:  "template",
			TemplateParameterUnknown:   "unknown",
			TemplateParameterKind(200): "unknown",
		} {
			assert.Equal(t, want, kind.String())
		}
	})

	t.Run("exception specification names and the throw answer", func(t *testing.T) {
		cases := []struct {
			kind   int32
			spec   ExceptionSpec
			text   string
			throws bool
		}{
			{exceptionSpecNone, ExceptionUnspecified, "unspecified", true},
			{exceptionSpecDynamicNone, ExceptionNone, "throw()", false},
			{exceptionSpecDynamic, ExceptionDynamic, "dynamic", true},
			{exceptionSpecMSAny, ExceptionDynamic, "dynamic", true},
			{exceptionSpecBasicNoexcept, ExceptionNoexcept, "noexcept", false},
			{exceptionSpecComputedNoexcept, ExceptionComputedNoexcept, "computed noexcept", true},
			{exceptionSpecUnevaluated, ExceptionUnknown, "unknown", true},
			{exceptionSpecUninstantiated, ExceptionUnknown, "unknown", true},
			{exceptionSpecUnparsed, ExceptionUnknown, "unknown", true},
			{exceptionSpecNoThrow, ExceptionNoThrow, "__declspec(nothrow)", false},
			{-1, ExceptionUnknown, "unknown", true},
		}
		for _, tc := range cases {
			spec := exceptionSpecFromKind(tc.kind)
			assert.Equal(t, tc.spec, spec, "kind %d", tc.kind)
			assert.Equal(t, tc.text, spec.String(), "kind %d", tc.kind)
			assert.Equal(t, tc.throws, spec.CanThrow(), "kind %d", tc.kind)
		}
	})
}

// TestTemplateArgumentString pins the argument's rendering: its spelling with
// libclang's punctuation spaces normalized, and its kind's name when libclang
// exposes no spelling at all.
func TestTemplateArgumentString(t *testing.T) {
	assert.Equal(t, "int", TemplateArgument{Kind: TemplateArgumentType, Spelling: "int"}.String())
	assert.Equal(t, "Q<int*>", TemplateArgument{Kind: TemplateArgumentType, Spelling: "Q<int *>"}.String())
	assert.Equal(t, "const unsigned long &", TemplateArgument{Kind: TemplateArgumentType, Spelling: "const unsigned long  &"}.String(),
		"a multiword type keeps the spaces that are part of it")
	assert.Equal(t, "integral", TemplateArgument{Kind: TemplateArgumentIntegral}.String(),
		"an argument with no spelling is named by its kind")

	assert.Equal(t,
		"mathx::Box<int,double>",
		specializationName("mathx::Box", []TemplateArgument{
			{Kind: TemplateArgumentType, Spelling: "int"},
			{Kind: TemplateArgumentType, Spelling: "double"},
		}))
	assert.Equal(t, "mathx::Box<>", specializationName("mathx::Box", nil))
}

// TestNormalizeTypeSpelling pins the punctuation folding: the identity of a
// specialization must not depend on the spaces libclang puts around pointers,
// references, commas and angle brackets.
func TestNormalizeTypeSpelling(t *testing.T) {
	assert.Equal(t, "int*", normalizeTypeSpelling("int *"))
	assert.Equal(t, "int*", normalizeTypeSpelling("int* "))
	assert.Equal(t, "int&", normalizeTypeSpelling("int &"))
	assert.Equal(t, "Q<int,double>", normalizeTypeSpelling("Q< int , double >"))
	assert.Equal(t, "unsigned long", normalizeTypeSpelling("  unsigned long "))
}
