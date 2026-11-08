package gen

import (
	"path/filepath"
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
		{"bigresult.hpp", "record by value as a result"},
		{"byvalue.hpp", "passing a record by value"},
		{"bitfield.hpp", "field accessors for an opaque record"},
		{"classtemplate.hpp", "class template"},
		{"incomplete.hpp", "not part of the generated API"},
		{"int128.hpp", "no Go value type"},
		{"longdouble.hpp", "long double"},
		{"manyfloats.hpp", "argument block"},
		{"manyints.hpp", "argument block"},
		{"membertemplate.hpp", "member function templates"},
		{"mixedaccess.hpp", "field accessors for an opaque record"},
		{"nestedfield.hpp", "field accessors for an opaque record"},
		{"nullptrarg.hpp", "nullptr_t"},
		{"opaquefield.hpp", "field accessors for an opaque record"},
		{"opaquevalue.hpp", "opaque record by value"},
		{"operand.hpp", "operators"},
		{"freeoperator.hpp", "operators"},
		{"int128ptr.hpp", "__int128"},
		{"packed.hpp", "field accessors for an opaque record"},
		{"pointerresult.hpp", "pointer to an opaque record as a result"},
		{"staticdata.hpp", "static data members"},
		{"templfunc.hpp", "template is not a type until it is instantiated"},
		{"unionrecord.hpp", "field accessors for an opaque record"},
		{"variadic.hpp", "variadic functions"},
		{"variadicmethod.hpp", "variadic functions"},
		{"voidptr.hpp", "void pointers"},
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

// TestEnumDiagnostic covers the enum root, which the model reports before any
// binding is built.
func TestEnumDiagnostic(t *testing.T) {
	cfg := Config{
		Headers: []string{"testdata/errors/enum.hpp"},
		Package: "err",
		Library: "liberr",
	}
	_, err := Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enum err::Color")
}
