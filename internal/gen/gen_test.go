package gen

import (
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderOutputs checks the four files a run produces, their names, and that
// the Go file is legal Go with the generated banner and invocation.
func TestRenderOutputs(t *testing.T) {
	outs, err := Render(mathxConfig())
	require.NoError(t, err)
	require.Len(t, outs, 4)

	byName := map[string][]byte{}
	for _, out := range outs {
		byName[out.Name] = out.Data
	}
	for _, name := range []string{GenFileName, AsmFileName, ShimHeaderFileName, ShimSourceFileName} {
		assert.Contains(t, byName, name)
	}

	gen := string(byName[GenFileName])
	assert.Contains(t, gen, GeneratedBanner)
	assert.Contains(t, gen, "cppgo gen -header testdata/mathx/vector.hpp -package mathx -library libmathx -out .")
	assert.Contains(t, gen, "package mathx")
	_, err = parser.ParseFile(token.NewFileSet(), GenFileName, gen, parser.ParseComments)
	require.NoError(t, err, "the generated file must be legal Go")

	assert.Contains(t, string(byName[AsmFileName]), "#include \"textflag.h\"")
	assert.Contains(t, string(byName[ShimHeaderFileName]), "#ifndef CPPGO_MATHX_SHIM_H")
	assert.Contains(t, string(byName[ShimSourceFileName]), "cppgo_last_error")
}

// TestRenderIsDeterministic checks that two runs produce byte-identical output,
// which is what makes `cppgo check` a usable gate.
func TestRenderIsDeterministic(t *testing.T) {
	first, err := Render(mathxConfig())
	require.NoError(t, err)
	second, err := Render(mathxConfig())
	require.NoError(t, err)
	require.Equal(t, len(first), len(second))
	for i := range first {
		assert.Equal(t, first[i].Name, second[i].Name)
		assert.Equal(t, string(first[i].Data), string(second[i].Data), first[i].Name)
	}
}

// TestGenerateWritesAndCheckAccepts runs the write path and then the check path.
func TestGenerateWritesAndCheckAccepts(t *testing.T) {
	dir := t.TempDir()
	cfg := mathxConfig()
	cfg.Out = dir
	cfg.Report = io.Discard
	require.NoError(t, Generate(cfg))
	for _, name := range []string{GenFileName, AsmFileName, ShimHeaderFileName, ShimSourceFileName} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, name)
	}
	check := cfg
	check.Check = true
	require.NoError(t, Generate(check))
}

// TestCheckReportsStaleFiles checks that a modified generated file is reported
// by name, and a missing one too.
func TestCheckReportsStaleFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := mathxConfig()
	cfg.Out = dir
	require.NoError(t, Generate(cfg))

	path := filepath.Join(dir, GenFileName)
	require.NoError(t, os.WriteFile(path, []byte("// stale\n"), 0o644))
	check := cfg
	check.Check = true
	err := Generate(check)
	require.Error(t, err)
	assert.Contains(t, err.Error(), GenFileName)

	require.NoError(t, os.Remove(filepath.Join(dir, ShimHeaderFileName)))
	err = Generate(check)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ShimHeaderFileName)
}

// TestGenerateWriteErrors covers the two filesystem failures the writer can
// report: an output path that is a file, and a generated name that exists as a
// directory.
func TestGenerateWriteErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	cfg := mathxConfig()
	cfg.Out = filepath.Join(file, "out")
	err := Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating output directory")

	out := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(out, GenFileName), 0o755))
	cfg.Out = out
	err = Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), GenFileName)
}

// TestRenderErrors covers the manifest validation the generator refuses before
// it parses anything.
func TestRenderErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"no header", Config{Package: "p", Library: "l"}, "-header is required"},
		{"bad package", Config{Headers: []string{"testdata/mathx/vector.hpp"}, Package: "9bad", Library: "l"}, "not a valid Go package name"},
		{"no library", Config{Headers: []string{"testdata/mathx/vector.hpp"}, Package: "p"}, "-library is required"},
		{"missing header", Config{Headers: []string{"testdata/nope.hpp"}, Package: "p", Library: "l"}, "nope.hpp"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Render(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestRenderMultipleHeaders parses the same header twice and still emits one
// declaration of each binding.
func TestRenderMultipleHeaders(t *testing.T) {
	cfg := mathxConfig()
	cfg.Headers = []string{"testdata/mathx/vector.hpp", "testdata/mathx/vector.hpp"}
	result, err := RenderResult(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, result.Outputs)
	assert.Len(t, result.Report, 9)
}

// TestInvocationRendersEveryFlag checks the regeneration command in the
// generated header names every manifest entry that shaped the package.
func TestInvocationRendersEveryFlag(t *testing.T) {
	cfg := Config{
		Headers:         []string{"a.hpp", "b.hpp"},
		Package:         "p",
		Library:         "libp",
		Out:             "out",
		CompileCommands: "build/compile_commands.json",
		Bind:            []string{"ns::A"},
		Exclude:         []string{"ns::B"},
		Rename:          []string{"ns::C::m=Met"},
	}
	invocation := cfg.invocation()
	for _, want := range []string{
		"-header a.hpp", "-header b.hpp", "-package p", "-library libp", "-out out",
		"-compile-commands build/compile_commands.json", "-bind ns::A", "-exclude ns::B",
		"-rename ns::C::m=Met",
	} {
		assert.Contains(t, invocation, want)
	}
}

// TestReportAndWarnings checks the per-binding report and the assumed-flags
// warning. The fixture has no compilation database, so the flags are the
// documented fallback and the generator must say so.
func TestReportAndWarnings(t *testing.T) {
	result, err := RenderResult(mathxConfig())
	require.NoError(t, err)
	require.NotEmpty(t, result.Report)
	assert.Len(t, result.Report, 9)
	var hasDirect, hasThunk bool
	for _, line := range result.Report {
		hasDirect = hasDirect || strings.Contains(line, " direct ")
		hasThunk = hasThunk || strings.Contains(line, " thunk ")
	}
	assert.True(t, hasDirect, "the report must name the direct carrier")
	assert.True(t, hasThunk, "the report must name the thunk carrier")
	require.NotEmpty(t, result.Warnings)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "assumption")
}
