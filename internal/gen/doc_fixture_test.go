package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDocFixtureIsEmittedAsGoDoc runs the whole rule against a header written the
// way a real one is: the C++ comment is the Go doc, the contract sentence leads
// only when the comment does not open with the identifier, the evidence line
// still closes the block, and the generated package compiles (TestFixturePackages
// Compile compiles every golden fixture, this one included).
func TestDocFixtureIsEmittedAsGoDoc(t *testing.T) {
	m := renderMap(t, fixtureConfig("docs", "docs.hpp"))
	source := m[GenFileName]

	cases := []struct {
		name string
		want string
	}{
		{
			name: "a struct's block comment, with its second paragraph",
			want: "// Point is docs::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.\n" +
				"//\n" +
				"// A point in the plane.\n" +
				"//\n" +
				"// The origin is at (0, 0).\n" +
				"type Point struct {\n" +
				"\t// The horizontal coordinate.\n" +
				"\tX float64\n" +
				"\t// The vertical coordinate.\n" +
				"\tY float64",
		},
		{
			name: "a class comment that does not open with the name",
			want: "// Circle is docs::Circle (sizeof 1). The object belongs to this Go value: obj is where it lives, Close destroys it, and a value obtained by value is a copy the shim constructed here.\n" +
				"//\n" +
				"// A circle.\n" +
				"//\n" +
				"// It has a center and a radius.\n" +
				"type Circle struct {",
		},
		{
			name: "a method comment that opens with the name is the whole doc",
			want: "// Area of the circle.\n//\n// C++: docs::Circle::area() const noexcept",
		},
		{
			name: "a returns command becomes a paragraph",
			want: "// Radius calls docs::Circle::radius.\n" +
				"//\n" +
				"// The radius.\n" +
				"//\n" +
				"// Returns the radius as a double.\n" +
				"//\n" +
				"// C++: docs::Circle::radius() const noexcept",
		},
		{
			name: "param commands keep the parameter names",
			want: "// Add two numbers.\n" +
				"//\n" +
				"// a the first addend\n" +
				"// b the second addend.\n" +
				"//\n" +
				"// Returns the sum.",
		},
		{
			name: "a plain line comment is not a doc comment",
			want: "// Subtract calls docs::subtract.\n//\n// C++: docs::subtract(double, double) noexcept",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, source, tc.want)
		})
	}

	// The constructor is documented the way the header wrote it.
	assert.Contains(t, source, "// NewCircle constructs a docs::Circle.\n//\n// Build a circle from its radius.")
}
