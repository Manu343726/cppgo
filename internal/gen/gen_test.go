package gen

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPkg is a minimal package the generator accepts.
const validPkg = `package x

//cppgo:library libx

// F does nothing in particular.
//
//cppgo:external f
func F(v int32) int32
`

func TestGenerateWritesAndCheckAccepts(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	out := filepath.Join(t.TempDir(), "nested", "out")

	require.NoError(t, Generate(Config{Dir: dir, Out: out}))

	// A nested output directory is created on demand.
	for _, name := range []string{GenFileName, AsmFileName, ShimFileName, SourceFileName} {
		assert.FileExists(t, filepath.Join(out, name))
	}
	assert.NoError(t, Generate(Config{Dir: dir, Out: out, Check: true}))
}

func TestGenerateIsIdempotent(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	out := t.TempDir()

	require.NoError(t, Generate(Config{Dir: dir, Out: out}))
	first := readDir(t, out)

	require.NoError(t, Generate(Config{Dir: dir, Out: out}))
	second := readDir(t, out)

	assert.Equal(t, first, second, "a second run must produce identical bytes")
	assert.NoError(t, Generate(Config{Dir: dir, Out: out, Check: true}))
}

func TestCheckReportsStaleFiles(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	out := t.TempDir()
	require.NoError(t, Generate(Config{Dir: dir, Out: out}))

	require.NoError(t, os.WriteFile(filepath.Join(out, GenFileName), []byte("stale\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(out, AsmFileName)))

	err := Generate(Config{Dir: dir, Out: out, Check: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of date")
	assert.Contains(t, err.Error(), GenFileName)
	assert.Contains(t, err.Error(), AsmFileName)
	assert.NotContains(t, err.Error(), ShimFileName)
}

func TestCheckOnEmptyOutputDirectory(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	err := Generate(Config{Dir: dir, Out: t.TempDir(), Check: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of date")
}

func TestRenderSortsOutputs(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	outs, err := Render(Config{Dir: dir, Out: t.TempDir()})
	require.NoError(t, err)
	require.Len(t, outs, 4)
	var names []string
	for _, o := range outs {
		names = append(names, o.Name)
	}
	assert.True(t, sort.StringsAreSorted(names), names)
}

func TestRenderDefaults(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	t.Chdir(dir)

	// Empty Dir means "." and an empty Out means "in place".
	require.NoError(t, Generate(Config{}))
	assert.FileExists(t, filepath.Join(dir, GenFileName))
	assert.NoFileExists(t, filepath.Join(dir, SourceFileName))

	data, err := os.ReadFile(filepath.Join(dir, "x.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "return cppgoCallF(v)")

	// Check mode has the same defaults.
	assert.NoError(t, Generate(Config{Check: true}))
}

func TestRenderMultipleSourceFiles(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"a.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external a\nfunc A()\n",
		"b.go": "package x\n\n//cppgo:external b\nfunc B()\n",
	})
	outs := renderMap(t, Config{Dir: dir, Out: t.TempDir()})
	assert.Contains(t, outs, SourceFileName)
	assert.Contains(t, outs, "zz_cppgo_stub_b.go")
	assert.Contains(t, outs["zz_cppgo_stub_b.go"], "func B() { cppgoCallB() }")
}

func TestRenderReportsSignatureErrorsWithPosition(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external f\nfunc F() []byte\n",
	})
	_, err := Render(Config{Dir: dir, Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x.go:")
	assert.Contains(t, err.Error(), "unsupported result type []byte")
}

func TestRenderErrors(t *testing.T) {
	_, err := Render(Config{Dir: filepath.Join(t.TempDir(), "missing"), Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading package directory")

	_, err = Render(Config{Dir: writePkg(t, map[string]string{"x.go": "package x\n"}), Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing //cppgo:library")
}

func TestGenerateWriteErrors(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})

	t.Run("output path is a file", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
		err := Generate(Config{Dir: dir, Out: filepath.Join(blocker, "sub")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating output directory")
	})

	t.Run("output directory is read only", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		out := t.TempDir()
		require.NoError(t, os.Chmod(out, 0o500))
		t.Cleanup(func() { _ = os.Chmod(out, 0o755) })
		err := Generate(Config{Dir: dir, Out: out})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "writing")
	})
}

func TestEmitSourceFormattingErrors(t *testing.T) {
	// The formatter is the last line of defence: an input that prints to
	// unparseable source must be reported, not written.
	_, err := gofmtSource("package x\n\nfunc (\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "formatting generated Go source")
}

func TestStripBannerText(t *testing.T) {
	in := GeneratedBanner + "\n\npackage x\n"
	assert.Equal(t, "package x\n", string(stripBannerText([]byte(in))))

	// A banner without the blank line after it.
	assert.Equal(t, "package x\n", string(stripBannerText([]byte(GeneratedBanner+"\npackage x\n"))))

	// Nothing to strip.
	src := []byte("// Package x does things.\npackage x\n")
	assert.Equal(t, src, stripBannerText(src))
	assert.Equal(t, []byte("package x"), stripBannerText([]byte("package x")))
}

func TestImportIdent(t *testing.T) {
	cases := map[string]string{
		`"fmt"`:                        "fmt",
		`"strings"`:                    "strings",
		`"gopkg.in/yaml.v3"`:           "yaml",
		`"github.com/x/go-spew"`:       "spew",
		`"github.com/x/json-go"`:       "json",
		`"github.com/russross/b/x/v2"`: "",
		`"gopkg.in/check.v1"`:          "check",
		`"github.com/x/weird-name"`:    "",
		`"not a path literal`:          "",
	}
	for in, want := range cases {
		assert.Equal(t, want, importIdent(in), in)
	}
}

func TestIsHelpers(t *testing.T) {
	assert.True(t, isVersionSuffix("v3"))
	assert.True(t, isVersionSuffix("V10"))
	assert.False(t, isVersionSuffix("v"))
	assert.False(t, isVersionSuffix("vx"))
	assert.False(t, isVersionSuffix("3"))
	assert.True(t, isGoIdent("_x9"))
	assert.False(t, isGoIdent("9x"))
	assert.False(t, isGoIdent(""))
	assert.False(t, isGoIdent("a-b"))
	assert.False(t, isGoIdent("a.b"))
}

// readDir returns every file in dir with its contents.
func readDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		out[e.Name()] = string(data)
	}
	return out
}
