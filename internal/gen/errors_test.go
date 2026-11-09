package gen

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerationErrors covers the input the contract says is an error: a
// construct that cannot be modelled at all, because the generic API itself has
// no shape for it. A declaration the build cannot carry is a warning instead
// (see TestDeclarationsTheBuildCannotCarryAreWarnings): that distinction is the
// whole point of declaration-level selection — one unusable declaration must
// not refuse the record or the overload set it lives in.
func TestGenerationErrors(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"classtemplate.hpp", "public field"},
		{"templnonetype.hpp", "public field"},
		{"templfuncdependent.hpp", "not a type this build classifies"},
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

// TestDeclarationsTheBuildCannotCarryAreWarnings is the other half: every one
// of these headers binds, and the declaration this build cannot carry is named
// on stderr instead of failing the run. The table is the measured list of
// constructs with no Go mapping yet — a `void *`, a pointer nobody owns, a
// smart pointer, a C array, a callback whose shape the table does not hold.
func TestDeclarationsTheBuildCannotCarryAreWarnings(t *testing.T) {
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
		{"incomplete.hpp", "not part of the generated API"},
		{"int128.hpp", "no Go value type"},
		{"int128ptr.hpp", "no Go value type"},
		{"longdouble.hpp", "no Go value type"},
		{"nullptrarg.hpp", "nullptr_t"},
		{"pointerresult.hpp", "pointer to an opaque record"},
		{"smartparam.hpp", "smart-pointer parameter"},
		{"smartresult.hpp", "smart-pointer result"},
		{"stdinout.hpp", "non-const standard-library reference"},
		{"stddeque.hpp", "outside the emitted table"},
		{"variadic.hpp", "variadic function"},
		{"variadicmethod.hpp", "variadic function"},
		{"voidptr.hpp", "void pointer"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			rendered, err := RenderResult(Config{
				Headers: []string{filepath.Join("testdata", "errors", tc.header)},
				Package: "err",
				Library: "liberr",
				Out:     ".",
			})
			require.NoError(t, err)
			joined := joinWarnings(rendered.Warnings)
			require.Contains(t, joined, tc.want, "the declaration must be named on stderr")
			assert.Contains(t, joined, "is not bound:", "a warning names the declaration and its reason")
			assert.NotEmpty(t, rendered.Outputs, "the package is written even when a declaration is not")
		})
	}
}

// TestUnimplementedWarningShape checks the contract's one-line warning shape:
// <qualified signature> is not bound: <what is wrong>; <fix>.
func TestUnimplementedWarningShape(t *testing.T) {
	cfg := Config{
		Headers: []string{"testdata/errors/stddeque.hpp"},
		Package: "err",
		Library: "liberr",
	}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	joined := joinWarnings(rendered.Warnings)
	pattern := regexp.MustCompile(`(?m)^err::consume\(std::deque<int>\) is not bound: .+; .+$`)
	assert.Regexp(t, pattern, joined)
	assert.Contains(t, joined, "outside the emitted table")
}

// joinWarnings is the fixture's warnings as one text, one per line.
func joinWarnings(warnings []string) string {
	joined := ""
	for _, warning := range warnings {
		joined += warning + "\n"
	}
	return joined
}
