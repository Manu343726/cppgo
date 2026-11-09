package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultArgumentsAreReadFromTheDeclaration proves the model carries the
// default-argument flag and the declaration's own text for it: libclang has no
// query for either, so the answer comes from the argument's printed
// declaration, and the "=" that separates the default is the top-level one.
func TestDefaultArgumentsAreReadFromTheDeclaration(t *testing.T) {
	skipWithoutLibclang(t)
	dir := t.TempDir()
	header := filepath.Join(dir, "defaults.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include <cstddef>
namespace dflt {

int none(int a, int b);
int one(int a, int b = 2);
int three(int a, int b = 2, double c = 1.5);
int named(int first, int second = 1, int third = 2);
int spaces(int a, int b = sizeof(int));
int comparison(bool flag = 1 == 1);
int template_default(int a = static_cast<int>(sizeof(double)));

struct Point { int x; };

class Shape {
public:
    Shape(int sides = 4);
    void scale(double factor = 2.0);
    int sides() const;
private:
    int sides_;
};

} // namespace dflt
`), 0o644))

	project, err := Open(Options{Header: header, Args: []string{"-std=c++17", "-xc++-header"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })

	cases := []struct {
		name      string
		defaulted []bool
		texts     []string
	}{
		{name: "dflt::none", defaulted: []bool{false, false}},
		{name: "dflt::one", defaulted: []bool{false, true}, texts: []string{"", "2"}},
		{name: "dflt::three", defaulted: []bool{false, true, true}, texts: []string{"", "2", "1.5"}},
		{name: "dflt::named", defaulted: []bool{false, true, true}, texts: []string{"", "1", "2"}},
		{name: "dflt::spaces", defaulted: []bool{false, true}, texts: []string{"", "sizeof(int)"}},
		{name: "dflt::comparison", defaulted: []bool{true}, texts: []string{"1 == 1"}},
		{name: "dflt::template_default", defaulted: []bool{true}, texts: []string{"static_cast<int>(sizeof(double))"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sym, ok := project.Lookup(tc.name)
			require.True(t, ok, "%s is not in the model", tc.name)
			require.Len(t, sym.Params, len(tc.defaulted))
			for i, want := range tc.defaulted {
				assert.Equal(t, want, sym.Params[i].HasDefault, "parameter %d of %s", i, tc.name)
				if want && i < len(tc.texts) {
					assert.Equal(t, tc.texts[i], sym.Params[i].DefaultText, "default %d of %s", i, tc.name)
				}
			}
		})
	}

	// A constructor's and a method's defaults are read the same way.
	ctor := project.Lookups("dflt::Shape::Shape")
	require.NotEmpty(t, ctor)
	assert.True(t, ctor[0].Params[0].HasDefault)
	assert.Equal(t, "4", ctor[0].Params[0].DefaultText)
	method := project.Lookups("dflt::Shape::scale")
	require.NotEmpty(t, method)
	assert.True(t, method[0].Params[0].HasDefault)
	assert.Equal(t, "2.", method[0].Params[0].DefaultText)
}

// TestDefaultArgumentSplitting pins the separator rule on the shapes a printed
// parameter takes.
func TestDefaultArgumentSplitting(t *testing.T) {
	cases := []struct {
		text  string
		value string
		ok    bool
	}{
		{text: "int mode", ok: false},
		{text: "int mode = 3", value: "3", ok: true},
		{text: "double factor = 2.0", value: "2.0", ok: true},
		{text: "std::vector<int> values = std::vector<int>()", value: "std::vector<int>()", ok: true},
		{text: "bool flag = 1 == 1", value: "1 == 1", ok: true},
		{text: "int (*callback)(int) = nullptr", value: "nullptr", ok: true},
		{text: "int value = a >= b", value: "a >= b", ok: true},
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
