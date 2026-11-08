package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDocCommentNormalisation pins what a C++ comment becomes in Go: the
// markers go, the Doxygen commands are replaced by the word a Go reader
// expects, paragraphs stay paragraphs, and the prose itself is not rewritten.
func TestDocCommentNormalisation(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "a one-line block comment",
			raw:  "/** A counter. */",
			want: "A counter.",
		},
		{
			name: "a block comment keeps its paragraphs",
			raw:  "/** A counter.\n *\n * It counts things.\n */",
			want: "A counter.\n\nIt counts things.",
		},
		{
			name: "line comments",
			raw:  "/// Add one.\n/// Soon.",
			want: "Add one.\nSoon.",
		},
		{
			name: "a bang line comment",
			raw:  "//! Kept.",
			want: "Kept.",
		},
		{
			name: "a plain line comment",
			raw:  "// Kept too.",
			want: "Kept too.",
		},
		{
			name: "the brief command goes, its text stays",
			raw:  "/** \\brief The brief. */",
			want: "The brief.",
		},
		{
			name: "details is dropped",
			raw:  "/**\n * @brief Short.\n * @details More.\n */",
			want: "Short.\n\nMore.",
		},
		{
			name: "param keeps the parameter name",
			raw:  "/** \\param value the value to add */",
			want: "value the value to add",
		},
		{
			name: "param direction is dropped",
			raw:  "/** @param[in,out] value the value */",
			want: "value the value",
		},
		{
			name: "returns becomes Returns",
			raw:  "/** \\returns the sum */",
			want: "Returns the sum",
		},
		{
			name: "note keeps the marker as a word",
			raw:  "/** \\note This allocates. */",
			want: "Note: This allocates.",
		},
		{
			name: "an unknown command is left alone",
			raw:  "/** \\unknowncommand text */",
			want: "\\unknowncommand text",
		},
		{
			name: "extra blank lines collapse",
			raw:  "/**\n *\n *\n * Text.\n *\n *\n * More.\n */",
			want: "Text.\n\nMore.",
		},
		{
			name: "an empty comment is empty",
			raw:  "/**  */",
			want: "",
		},
		{
			name: "a bare star line is not text",
			raw:  "/**\n *\n * Text.\n *\n */",
			want: "Text.",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, docComment(tc.raw))
		})
	}
}

// TestDocBlock pins the rule that decides how a comment and the generated
// contract sentence are laid out: a comment that opens with the identifier is
// the whole doc; otherwise the sentence leads and the comment follows as its own
// paragraph; with no comment only the sentence is emitted.
func TestDocBlock(t *testing.T) {
	cases := []struct {
		name     string
		goName   string
		fallback string
		raw      string
		want     string
	}{
		{
			name:     "no comment emits the contract sentence",
			goName:   "Radius",
			fallback: "Radius calls mathx::Circle::radius.",
			raw:      "",
			want:     "// Radius calls mathx::Circle::radius.",
		},
		{
			name:     "a comment that opens with the identifier is the doc",
			goName:   "Area",
			fallback: "Area calls mathx::Circle::area.",
			raw:      "/** Area of the shape. */",
			want:     "// Area of the shape.",
		},
		{
			name:     "an identifier prefix is not the same word",
			goName:   "Area",
			fallback: "Area calls mathx::Circle::area.",
			raw:      "/** Areas differ. */",
			want:     "// Area calls mathx::Circle::area.\n//\n// Areas differ.",
		},
		{
			name:     "a comment that does not open with the identifier follows the sentence",
			goName:   "Radius",
			fallback: "Radius calls mathx::Circle::radius.",
			raw:      "/** The radius, squared. */",
			want:     "// Radius calls mathx::Circle::radius.\n//\n// The radius, squared.",
		},
		{
			name:     "paragraphs are separated by a bare comment line",
			goName:   "Scale",
			fallback: "",
			raw:      "/** Scale.\n *\n * By a factor.\n */",
			want:     "// Scale.\n//\n// By a factor.",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, docBlock(tc.goName, tc.fallback, tc.raw))
		})
	}
}

// TestCommentLinesAndIndent pins the two helpers an enum constant uses: the
// comment is a "//" block, and inside a const group it is indented by one tab.
func TestCommentLinesAndIndent(t *testing.T) {
	assert.Equal(t, "// Fast.\n", commentLines("/** Fast. */"))
	assert.Equal(t, "", commentLines(""))
	assert.Equal(t, "\t// Fast.\n\t// And safe.\n", indentLines(commentLines("/// Fast.\n/// And safe."), "\t"))
}
