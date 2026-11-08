package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestDeclErrorShapes checks the contract's one-line shape and its fallbacks.
func TestDeclErrorShapes(t *testing.T) {
	assert.Equal(t, "cppgo: f.hpp:3: sym: what; fix", declError("f.hpp", 3, "sym", pending("what", "fix")).Error())
	assert.Equal(t, "cppgo: line 3: sym: what; fix", declError("", 3, "sym", pending("what", "fix")).Error())
	assert.Equal(t, "cppgo: <unknown>: sym: what; fix", declError("", 0, "sym", pending("what", "fix")).Error())

	record := cxx.Record{QualifiedName: "ns::R", DeclFile: "r.hpp", DeclLine: 7}
	assert.Equal(t, "cppgo: r.hpp:7: ns::R: what; fix", recordError(record, pending("what", "fix")).Error())

	symbol := cxx.Symbol{QualifiedName: "ns::f", DeclFile: "s.hpp", DeclLine: 2}
	assert.Equal(t, "cppgo: s.hpp:2: ns::f(): what; fix", symbolError(symbol, pending("what", "fix")).Error())
}

// TestShimSymbol maps a diagnostic line back to its thunk.
func TestShimSymbol(t *testing.T) {
	spans := []thunkSpan{{start: 10, end: 20, symbol: "A"}, {start: 22, end: 30, symbol: "B"}}
	assert.Equal(t, "A", shimSymbol(spans, 10))
	assert.Equal(t, "A", shimSymbol(spans, 15))
	assert.Equal(t, "B", shimSymbol(spans, 30))
	assert.Equal(t, ShimSourceFileName, shimSymbol(spans, 5))
	assert.Equal(t, ShimSourceFileName, shimSymbol(spans, 99))
}

// TestShimDiagnostic checks the location rendering and the empty-file fallback.
func TestShimDiagnostic(t *testing.T) {
	dir := "/scratch/cppgo-shim"
	err := shimDiagnostic(dir, "ns::f", &cxx.Diagnostic{
		File:    filepath.Join(dir, ShimSourceFileName),
		Line:    4,
		Message: "boom",
	})
	assert.Equal(t, "cppgo: zz_cppgo_shim.cpp:4: ns::f: the generated shim does not compile; boom", err.Error())

	err = shimDiagnostic(dir, "ns::f", &cxx.Diagnostic{Line: 9, Message: "boom"})
	assert.Equal(t, "cppgo: zz_cppgo_shim.cpp:9: ns::f: the generated shim does not compile; boom", err.Error())
}

// TestShimFlags checks the language replacement and the include paths.
func TestShimFlags(t *testing.T) {
	model := &packageModel{
		flags:   []string{"-std=c++17", "-x", "c++-header", "-xc++", "-I/tmp/project"},
		headers: []string{"testdata/mathx/vector.hpp"},
	}
	args := model.shimFlags("/scratch")
	assert.Contains(t, args, "-std=c++17")
	assert.Contains(t, args, "-I/tmp/project")
	assert.NotContains(t, args, "c++-header")
	assert.NotContains(t, args, "-xc++")
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-x" {
			assert.Equal(t, "c++", args[i+1])
		}
	}
	assert.Contains(t, args, "-I")
	assert.Contains(t, args, "/scratch")
	abs, err := filepath.Abs("testdata/mathx")
	require.NoError(t, err)
	assert.Contains(t, args, abs)
}

// TestFirstError checks the severity filter.
func TestFirstError(t *testing.T) {
	assert.Nil(t, firstError(nil))
	assert.Nil(t, firstError([]cxx.Diagnostic{{Severity: cxx.DiagnosticWarning, Message: "w"}}))
	diagnostics := []cxx.Diagnostic{
		{Severity: cxx.DiagnosticWarning, Message: "w"},
		{Severity: cxx.DiagnosticError, Message: "e"},
	}
	first := firstError(diagnostics)
	require.NotNil(t, first)
	assert.Equal(t, "e", first.Message)
}

// TestValidateShimWithoutFlags checks the emitter-test model, which has no
// headers and therefore nothing to compile against.
func TestValidateShimWithoutFlags(t *testing.T) {
	assert.NoError(t, (&packageModel{}).validateShim(nil, nil, nil))
}

// TestParseShimWriteError covers the scratch-file failure.
func TestParseShimWriteError(t *testing.T) {
	model := &packageModel{flags: []string{"-std=c++17"}}
	_, err := model.parseShim(filepath.Join(t.TempDir(), "missing", "dir"), "x", []byte("int x;"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing")
}
