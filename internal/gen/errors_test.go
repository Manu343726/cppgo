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
		{"nullptrarg.hpp", "nullptr_t"},
		{"opaquevalue.hpp", "is passed by value"},
		{"pointerresult.hpp", "pointer to an opaque record"},
		{"smartparam.hpp", "smart-pointer parameter"},
		{"smartresult.hpp", "smart-pointer result"},
		{"stdinout.hpp", "non-const standard-library reference"},
		{"templfuncdependent.hpp", "not a type this build classifies"},
		{"templnonetype.hpp", "public field"},
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

// TestOmittedMembersAreReported covers the members a record or a template keeps
// but cannot bind: a member function template has no signature of its own, an
// operator of a class template has one per instantiation, and a dependent type
// has none until it is substituted. Each is omitted from the API and named in
// the report, which is a different answer from failing the whole header — that
// answer is what made a real header unbindable.
func TestOmittedMembersAreReported(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"membertemplate.hpp", "err::WithMember::identity is a member function template"},
		{"tempoperator.hpp", "Eigen"},
		{"tempmembertemplate.hpp", "err::Holder::identity is a member function template"},
		{"tempdependent.hpp", `std::vector<T>" is not a type this build classifies`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			if tc.want == "Eigen" {
				// The operator case: the warning names the declaration, which
				// libclang spells with the template parameters.
				rendered, err := RenderResult(Config{
					Headers: []string{filepath.Join("testdata", "errors", tc.header)},
					Package: "err",
					Library: "liberr",
				})
				require.NoError(t, err)
				require.NotEmpty(t, rendered.Warnings)
				assert.Contains(t, rendered.Warnings[0], "operator")
				return
			}
			rendered, err := RenderResult(Config{
				Headers: []string{filepath.Join("testdata", "errors", tc.header)},
				Package: "err",
				Library: "liberr",
			})
			require.NoError(t, err)
			joined := ""
			for _, warning := range rendered.Warnings {
				joined += warning + "\n"
			}
			assert.Contains(t, joined, tc.want)
		})
	}
}
