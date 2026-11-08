package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestEnumModelIsANamedIntegerType pins the Go shape of every enum declaration
// in the fixture: the named type, the underlying width from libclang, and one
// typed constant per enumerator, including negative, unsigned and typedef'd
// spellings.
func TestEnumModelIsANamedIntegerType(t *testing.T) {
	m := renderMap(t, fixtureConfig("enums", "enums.hpp"))
	source := m[GenFileName]

	cases := []struct {
		name string
		want []string
	}{
		{
			name: "scoped enum with an explicit int underlying type",
			want: []string{
				"type Axis int32",
				"AxisX Axis = 0",
				"AxisY Axis = 1",
				"AxisZ Axis = 2",
			},
		},
		{
			name: "unscoped enum with a compiler-chosen underlying type",
			want: []string{
				"type Flags uint32",
				"FlagsNoFlags Flags = 0",
				"FlagsRead    Flags = 1",
				"FlagsWrite   Flags = 2",
				"FlagsExecute Flags = 7",
			},
		},
		{
			name: "byte-sized underlying type and a 255 maximum",
			want: []string{
				"type Mode uint8",
				"ModeOff     Mode = 0",
				"ModeOn      Mode = 200",
				"ModeMaximum Mode = 255",
			},
		},
		{
			name: "negative and non-contiguous enumerators",
			want: []string{
				"type Offset int16",
				"OffsetBehind Offset = -3",
				"OffsetFar    Offset = 900",
			},
		},
		{
			name: "C-style typedef enum",
			want: []string{
				"type ColorT uint32",
				"ColorTRed   ColorT = 10",
				"ColorTGreen ColorT = 20",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.want {
				assert.Contains(t, source, want)
			}
		})
	}
}

// TestEnumInASignatureCrossesAsItsUnderlyingInteger checks the two halves of a
// signature that names an enum: the Go type is the named one, and the wire and
// the shim use the underlying integer, because that is what the ABI carries.
func TestEnumInASignatureCrossesAsItsUnderlyingInteger(t *testing.T) {
	m := renderMap(t, fixtureConfig("enums", "enums.hpp"))
	source := m[GenFileName]
	shim := m[ShimSourceFileName]

	assert.Contains(t, source, "func Opposite(axis Axis) Axis {")
	assert.Contains(t, source, "func HasFlag(flags Flags, flag Flags) bool {")
	assert.Contains(t, source, "func NextMode(mode Mode) Mode {")

	// The wire word is the underlying integer type of the enum.
	assert.Contains(t, shim, "static_cast<int32_t>(")
	assert.Contains(t, shim, "static_cast<::enums::Axis>(")
	assert.Contains(t, shim, "static_cast<::enums::Flags>(")
	assert.Contains(t, shim, "static_cast<::enums::Mode>(")
}

// TestEnumDocCommentBecomesTheGoDoc measures the documentation rule: a comment
// that opens with the identifier is the whole doc, and one that does not keeps
// the contract sentence and follows it as a paragraph.
func TestEnumDocCommentBecomesTheGoDoc(t *testing.T) {
	m := renderMap(t, fixtureConfig("enums", "enums.hpp"))
	source := m[GenFileName]

	assert.Contains(t, source, "// Axis is the coordinate axis of a point.\ntype Axis int32")
	assert.Contains(t, source, "// ColorT is enums::Color_t, an enum with underlying type uint32.\n//\n// Color_t tests the C-style typedef spelling.\ntype ColorT uint32")
	assert.Contains(t, source, "\t// The x axis.\n\tAxisX Axis = 0")
	assert.Contains(t, source, "\t// The y axis.\n\tAxisY Axis = 1")
}

// TestEnumRefusalIsGone is the negative half: an enum declaration no longer
// stops the generation with the "pass the underlying integer" message.
func TestEnumRefusalIsGone(t *testing.T) {
	outs, err := Render(fixtureConfig("enums", "enums.hpp"))
	require.NoError(t, err)
	require.NotEmpty(t, outs)
	for _, out := range outs {
		assert.NotContains(t, string(out.Data), "enums are not emitted by this build yet")
	}
}

// TestEnumBindingWithoutConstants covers the one enum form the model cannot
// evaluate: a specialization's nested enum whose initialisers a pattern cannot
// be read for. The error names the enum and the fix instead of emitting zeroes.
func TestEnumBindingWithoutConstants(t *testing.T) {
	b := &builder{cfg: Config{}, records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.rawEnums = []cxx.Enum{{
		QualifiedName:  "mathx::Box<int>::Kind",
		ConstantsKnown: false,
		Underlying:     cxx.Param{Kind: cxx.TypeInteger, Spelling: "int", SizeBytes: 4},
	}}
	err := b.buildEnums()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mathx::Box<int>::Kind")
	assert.Contains(t, err.Error(), "literal initialisers")
}

// TestEnumUnderlyingTypeWithoutAGoWidth refuses an enum whose underlying type
// has no Go value type rather than emitting a value Go would reinterpret.
func TestEnumUnderlyingTypeWithoutAGoWidth(t *testing.T) {
	b := &builder{cfg: Config{}, records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.enumGoNames = map[string]string{"wide::Kind": "Kind"}
	_, err := b.enumModelFor(cxx.Enum{
		QualifiedName:  "wide::Kind",
		ConstantsKnown: true,
		Underlying:     cxx.Param{Kind: cxx.TypeInteger, Spelling: "__int128", SizeBytes: 16},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Go value type")
}

// TestEnumUnderlyingTypeThatIsNotAnInteger refuses an underlying type the model
// does not classify as an integer.
func TestEnumUnderlyingTypeThatIsNotAnInteger(t *testing.T) {
	b := &builder{cfg: Config{}, records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.enumGoNames = map[string]string{"odd::Kind": "Kind"}
	_, err := b.enumModelFor(cxx.Enum{
		QualifiedName:  "odd::Kind",
		ConstantsKnown: true,
		Underlying:     cxx.Param{Kind: cxx.TypeFloat, Spelling: "double", SizeBytes: 8},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an integer")
}
