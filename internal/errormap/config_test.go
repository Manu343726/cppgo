package errormap_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/errormap"
)

// signatures is what the generator knows about testdata/errlib.hpp, in the
// shape libclang hands it: the qualified name, the return spelling, and every
// parameter with its name and whether it can carry a value back. In the emitter
// these come from the parse; here they are written out so the config validation
// can be measured without libclang in the loop.
func signatures() []errormap.Signature {
	return []errormap.Signature{
		{Name: "ns::open_file", Returns: "std::error_code", Params: []errormap.Param{
			{Name: "path"}, {Name: "out_fd", Pointer: true},
		}},
		{Name: "ns::parse_int", Returns: "std::expected<int, std::error_code>", Params: []errormap.Param{
			{Name: "text"},
		}},
		{Name: "ns::divide", Returns: "std::int32_t", Params: []errormap.Param{
			{Name: "a"}, {Name: "b"},
		}},
		{Name: "ns::connect", Returns: "std::int32_t", Params: []errormap.Param{
			{Name: "host"},
		}},
		{Name: "ns::parse_point", Returns: "bool", Params: []errormap.Param{
			{Name: "text"}, {Name: "out", Pointer: true}, {Name: "status", Pointer: true},
		}},
		{Name: "ns::load", Returns: "ns::Result<int>", Params: []errormap.Param{
			{Name: "key"},
		}},
	}
}

// infer is the declaration-only half: what the generator concludes from the
// return type and the exception specification, before any config is consulted.
func infer(sig errormap.Signature) errormap.Inference {
	switch sig.Name {
	case "ns::open_file":
		return errormap.Inference{Kind: errormap.KindErrorCode, Fallible: true}
	case "ns::parse_int":
		return errormap.Inference{Kind: errormap.KindExpected, Fallible: true}
	case "ns::divide", "ns::connect":
		return errormap.Inference{Kind: errormap.KindException, Fallible: true}
	case "ns::load":
		// ns::Result<int> has has_value/value/error, which Infer recognises.
		kind, ok := errormap.Infer(errormap.Shape{HasValue: true, Value: true, Error: true})
		if !ok {
			panic("ns::Result<int> must be recognised")
		}
		return errormap.Inference{Kind: kind, Fallible: true}
	default:
		// A bool with out parameters says nothing by itself.
		return errormap.Inference{}
	}
}

// resolve loads the package's config and applies it to the fixture's
// declarations, returning the rules by symbol. It is the generator's step, and
// it fails the test rather than returning an error because every test that uses
// it is about what happens after a valid config.
func resolve(t *testing.T) map[string]errormap.Rule {
	t.Helper()

	data, err := os.ReadFile("testdata/cppgo.yaml")
	require.NoError(t, err)

	cfg, err := errormap.Load(data)
	require.NoError(t, err)

	rules, err := cfg.Apply(signatures(), infer)
	require.NoError(t, err)

	byName := make(map[string]errormap.Rule, len(rules))
	for _, rule := range rules {
		byName[rule.Symbol] = rule
	}
	return byName
}

// TestApplyResolvesEveryRow pins the whole table at once: which mechanism each
// declaration got, whether it was inferred or configured, and the report line
// the generator writes.
func TestApplyResolvesEveryRow(t *testing.T) {
	rules := resolve(t)

	cases := []struct {
		symbol     string
		kind       errormap.Kind
		configured bool
		report     string
	}{
		{"ns::open_file", errormap.KindErrorCode, false, "ns::open_file\terror_code\tinferred\tvalue=return error=return"},
		{"ns::parse_int", errormap.KindExpected, false, "ns::parse_int\texpected\tinferred\tvalue=return error=return"},
		{"ns::divide", errormap.KindException, false, "ns::divide\texception\tinferred\tvalue=return error=return"},
		{"ns::connect", errormap.KindException, false, "ns::connect\texception\tinferred\tvalue=return error=return"},
		{"ns::load", errormap.KindCustom, false, "ns::load\tcustom\tinferred\tvalue=return error=return"},
		{"ns::parse_point", errormap.KindOutParam, true, "ns::parse_point\tout_param\tconfigured\tvalue=1 error=2"},
	}

	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			rule := rules[tc.symbol]
			assert.Equal(t, tc.kind, rule.Kind)
			assert.Equal(t, tc.configured, rule.Configured)
			assert.Equal(t, tc.report, rule.Report())
		})
	}

	assert.Len(t, rules, len(signatures()), "every declaration has a rule")
	assert.Equal(t, "ns::ParseStatus", rules["ns::parse_point"].Projection,
		"the config's projection is carried into the rule")
}

// TestConfigValidation covers every way an entry can be wrong. Each of them is a
// generation error: no output is written, so a config cannot invent a signature.
func TestConfigValidation(t *testing.T) {
	const base = `
errors:
  "ns::parse_point":
    kind: out_param
    value: 1
    error: 2
`

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "wrong parameter index",
			yaml: `
errors:
  "ns::parse_point": {kind: out_param, value: 1, error: 5}
`,
			want: `ns::parse_point: error channel: parameter 5 does not exist; the declaration takes 0:text, 1:out, 2:status`,
		},
		{
			name: "wrong parameter name",
			yaml: `
errors:
  "ns::parse_point": {kind: out_param, value: 1, error: code}
`,
			want: `ns::parse_point: error channel: parameter "code" does not exist; the declaration takes 0:text, 1:out, 2:status`,
		},
		{
			name: "value channel that cannot carry a value",
			yaml: `
errors:
  "ns::parse_point": {kind: out_param, value: 0, error: 2}
`,
			want: `value channel "0" is parameter 0, which is not a pointer or reference`,
		},
		{
			name: "error channel that cannot report a failure",
			yaml: `
errors:
  "ns::parse_point": {kind: out_param, value: 1, error: 0}
`,
			want: `error channel "0" is parameter 0, which is not a pointer or reference`,
		},
		{
			name: "a symbol this package does not bind",
			yaml: `
errors:
  "ns::parse": {kind: out_param, value: 1, error: 2}
`,
			want: `config names "ns::parse", which is not a declaration this package binds`,
		},
		{
			name: "an inferred kind cannot be configured",
			yaml: `
errors:
  "ns::divide": {kind: exception}
`,
			want: `ns::divide: exceptions are inferred from the declaration and cannot be configured`,
		},
		{
			name: "a kind that returns its error takes no channels",
			yaml: `
errors:
  "ns::open_file": {kind: error_code, error: 0}
`,
			want: `ns::open_file: kind "error_code" returns its value and its error, so it takes no channels`,
		},
		{
			name: "a status kind reports through its return value",
			yaml: `
errors:
  "ns::open_file": {kind: status, error: 0}
`,
			want: `ns::open_file: kind "status" reports through the return value, not through "0"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := errormap.Load([]byte(tc.yaml))
			require.NoError(t, err)

			_, err = cfg.Apply(signatures(), infer)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	// The base entry is valid, so the fixture in the file is the one the
	// validation accepts.
	cfg, err := errormap.Load([]byte(base))
	require.NoError(t, err)
	_, err = cfg.Apply(signatures(), infer)
	require.NoError(t, err)
}

// TestLoadRejectsMalformedConfig covers the file-level errors: a kind that does
// not exist and an entry with no kind at all.
func TestLoadRejectsMalformedConfig(t *testing.T) {
	_, err := errormap.Load([]byte(`errors: {"ns::parse_point": {kind: guess}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown kind "guess"`)

	_, err = errormap.Load([]byte(`errors: {"ns::parse_point": {value: 1}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kind")

	_, err = errormap.Load([]byte(`errors: [`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config")
}

// TestInfer covers the shape rule: only the three members together make a
// Result-like type, and anything else is not an error type at all.
func TestInfer(t *testing.T) {
	cases := []struct {
		name  string
		shape errormap.Shape
		kind  errormap.Kind
		ok    bool
	}{
		{"all three members", errormap.Shape{HasValue: true, Value: true, Error: true}, errormap.KindCustom, true},
		{"no error accessor", errormap.Shape{HasValue: true, Value: true}, "", false},
		{"no has_value", errormap.Shape{Value: true, Error: true}, "", false},
		{"empty", errormap.Shape{}, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, ok := errormap.Infer(tc.shape)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.kind, kind)
		})
	}
}

// TestConfiguredSymbolsAreSorted keeps the report deterministic.
func TestConfiguredSymbolsAreSorted(t *testing.T) {
	cfg, err := errormap.Load([]byte(`
errors:
  "ns::parse_point": {kind: out_param, value: 1, error: 2}
  "ns::decode": {kind: out_param, value: 1, error: 2}
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"ns::decode", "ns::parse_point"}, cfg.Symbols())
	assert.True(t, strings.HasPrefix(cfg.Symbols()[0], "ns::decode"))
}

// TestChannelAndReportEdges covers the parts of the report and the validation
// that only a degenerate signature reaches: a declaration with no parameters,
// one whose parameters are unnamed, a negative channel index, and a nil config.
func TestChannelAndReportEdges(t *testing.T) {
	noParams := errormap.Signature{Name: "ns::reset", Returns: "bool"}
	entry := errormap.Entry{Kind: errormap.KindOutParam, Value: "0"}

	cfg := &errormap.Config{Errors: map[string]errormap.Entry{"ns::reset": entry}}
	_, err := cfg.Resolve(noParams, errormap.Inference{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameter 0 does not exist; the declaration takes no parameters")

	unnamed := errormap.Signature{
		Name:    "ns::reset",
		Returns: "bool",
		Params:  []errormap.Param{{Pointer: true}},
	}
	cfg = &errormap.Config{Errors: map[string]errormap.Entry{
		"ns::reset": {Kind: errormap.KindOutParam, Value: "out"},
	}}
	_, err = cfg.Resolve(unnamed, errormap.Inference{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `parameter "out" does not exist; the declaration takes 0:?`)

	cfg = &errormap.Config{Errors: map[string]errormap.Entry{
		"ns::reset": {Kind: errormap.KindOutParam, Value: "-1"},
	}}
	_, err = cfg.Resolve(unnamed, errormap.Inference{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameter -1 does not exist")

	// A nil config is a package with no entries: every declaration is
	// resolved from its own shape and nothing is reported as configured.
	var none *errormap.Config
	rule, err := none.Resolve(
		errormap.Signature{Name: "ns::divide", Returns: "std::int32_t"},
		errormap.Inference{Kind: errormap.KindException, Fallible: true})
	require.NoError(t, err)
	assert.Equal(t, errormap.KindException, rule.Kind)
	assert.False(t, rule.Configured)

	assert.Nil(t, none.Symbols())
	require.NoError(t, none.Validate(signatures()))
}

// TestChannelByName pins the other spelling of a channel: a parameter's name
// instead of its index.
func TestChannelByName(t *testing.T) {
	cfg, err := errormap.Load([]byte(`
errors:
  "ns::parse_point": {kind: out_param, value: out, error: status}
`))
	require.NoError(t, err)

	rule, err := cfg.Resolve(signatures()[4], errormap.Inference{})
	require.NoError(t, err)
	assert.Equal(t, 1, rule.Value, "out is parameter 1")
	assert.Equal(t, 2, rule.Error, "status is parameter 2")
}
