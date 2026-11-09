package gen

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerationErrors covers every construct this build does not emit yet and
// every input the contract says is an error. Each case must name itself, not
// fail with an opaque parse message.
func TestGenerationErrors(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"bitfield.hpp", "public field"},
		{"callbackargs.hpp", "more than 6 INTEGER arguments"},
		{"callbackfloat.hpp", "take a double instead"},
		{"callbackpointer.hpp", "of a callback binding is a"},
		{"callbackmanyfloats.hpp", "more than 8 SSE arguments"},
		{"callbackrecord.hpp", "is not a scalar"},
		{"callbackrecordresult.hpp", "is not a scalar"},
		{"callbackstring.hpp", "is not a scalar or a known record"},
		{"callbackresult.hpp", "is a function or std::function result"},
		{"callbackstdresult.hpp", "returns a standard-library value and takes a callback"},
		{"classtemplate.hpp", "public field"},
		{"incomplete.hpp", "not part of the generated API"},
		{"int128.hpp", "no Go value type"},
		{"int128ptr.hpp", "no Go value type"},
		{"longdouble.hpp", "no Go value type"},
		{"manyfloats.hpp", "argument block"},
		{"manyints.hpp", "argument block"},
		{"membertemplate.hpp", "member function template"},
		{"mixedaccess.hpp", "public field"},
		{"nestedfield.hpp", "public field"},
		{"nullptrarg.hpp", "nullptr_t"},
		{"opaquefield.hpp", "public field"},
		{"opaquevalue.hpp", "is passed by value"},
		{"packed.hpp", "public field"},
		{"pointerresult.hpp", "pointer to an opaque record"},
		{"smartparam.hpp", "smart-pointer parameter"},
		{"smartresult.hpp", "smart-pointer result"},
		{"stdinout.hpp", "non-const standard-library reference"},
		{"tempdependent.hpp", "not a type this build classifies"},
		{"templfuncdependent.hpp", "not a type this build classifies"},
		{"tempmembertemplate.hpp", "member function template"},
		{"templnonetype.hpp", "public field"},
		{"tempoperator.hpp", "operators and conversion operators"},
		{"unionrecord.hpp", "public field"},
		{"variadic.hpp", "variadic function"},
		{"variadicmethod.hpp", "variadic function"},
		{"voidptr.hpp", "void pointer"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			cfg := Config{
				Headers: []string{filepath.Join("testdata", "errors", tc.header)},
				Package: "err",
				Library: "liberr",
				Out:     ".",
			}
			_, err := Render(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestUnimplementedErrorShape checks the contract's exact one-line shape:
// cppgo: <file>:<line>: <symbol>: <what is wrong>; <fix>.
func TestUnimplementedErrorShape(t *testing.T) {
	cfg := Config{
		Headers: []string{"testdata/errors/stddeque.hpp"},
		Package: "err",
		Library: "liberr",
	}
	_, err := build(cfg)
	require.Error(t, err)
	pattern := regexp.MustCompile(`^cppgo: .+:\d+: .+: .+; .+$`)
	assert.Regexp(t, pattern, err.Error())
	assert.Contains(t, err.Error(), "outside the emitted table")
}

// TestInheritedAndNestedRejections covers the two constructs the verifier saw
// silently omitted.
func TestInheritedAndNestedRejections(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"inherited.hpp", "inherited members and base views"},
		{"nestedtype.hpp", "nested inside a record"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			cfg := Config{
				Headers: []string{filepath.Join("testdata", "errors", tc.header)},
				Package: "err",
				Library: "liberr",
			}
			_, err := build(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
