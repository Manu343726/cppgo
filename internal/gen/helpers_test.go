package gen

import (
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// banner is the generated-file marker as it appears in a written file.
const banner = GeneratedBanner + "\n\n"

// writePkg materialises a package directory from a name -> contents map.
func writePkg(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

// mustParse parses a directory that is expected to be valid.
func mustParse(t *testing.T, dir string) *packageInfo {
	t.Helper()
	pkg, err := parsePackage(dir)
	require.NoError(t, err)
	return pkg
}

// testFunc parses a single //cppgo:external declaration and returns its model.
func testFunc(t *testing.T, decl string) *Func {
	t.Helper()
	dir := writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external sym\n" + decl + "\n",
	})
	pkg := mustParse(t, dir)
	require.Len(t, pkg.Funcs, 1)
	return pkg.Funcs[0]
}

// testSig parses a declaration and computes its calling convention.
func testSig(t *testing.T, decl string) *signature {
	t.Helper()
	sig, err := buildSignature(testFunc(t, decl))
	require.NoError(t, err)
	return sig
}

// renderMap runs the generator and returns its files by name.
func renderMap(t *testing.T, cfg Config) map[string]string {
	t.Helper()
	outs, err := Render(cfg)
	require.NoError(t, err)
	m := make(map[string]string, len(outs))
	for _, o := range outs {
		m[o.Name] = string(o.Data)
	}
	return m
}

// mustExpr parses a type expression.
func mustExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	require.NoError(t, err)
	return expr
}

// readFileString reads a file as a string.
func readFileString(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	return string(data), err
}
