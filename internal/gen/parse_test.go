package gen

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDirectives(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"a.go": `// Package mathx binds a C++ library.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx

// AddI64 adds two integers.
//
//cppgo:external add_i64
func AddI64(a, b int64) int64

// Lookup returns an opaque object.
//
//cppgo:external lookup
func Lookup(key string, _ int32) cppgo.Handle

// Async does something without a result.
//
//cppgo:external async
func Async()

// Split parses its results.
//
//cppgo:external split
func Split(s string) (parts int32, err error)
`,
		"b.go": `package mathx

//cppgo:external dot
func Dot(a []float64) (float64, error)
`,
	})

	pkg := mustParse(t, dir)
	assert.Equal(t, "mathx", pkg.Name)
	assert.Equal(t, "libmathx", pkg.Library)
	require.Len(t, pkg.Funcs, 5)

	// Functions are sorted by Go name so the output is deterministic.
	var names []string
	for _, f := range pkg.Funcs {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"AddI64", "Async", "Dot", "Lookup", "Split"}[:0:0], names[:0])
	assert.Equal(t, []string{"AddI64", "Async", "Dot", "Lookup", "Split"}, names)

	add := pkg.Funcs[0]
	assert.Equal(t, "add_i64", add.Symbol)
	assert.Equal(t, []string{"// AddI64 adds two integers."}, add.Doc)
	assert.Equal(t, "cppgo_add_i64", add.cName())
	assert.Equal(t, "cppgo_add_i64", add.trampName())
	assert.Equal(t, "cppgoCallAddI64", add.wrapperName())
	assert.Equal(t, "cppgoSymAddI64", add.symVar())
	assert.Equal(t, "func AddI64(a int64, b int64) int64", add.signature())
	require.Len(t, add.Params, 2)
	assert.Equal(t, "a", add.Params[0].Name)
	assert.Equal(t, "int64", add.Params[0].Info.goType)
	require.Len(t, add.Results, 1)
	assert.Equal(t, "int64", add.Results[0].Info.goType)

	lookup := pkg.Funcs[3]
	require.Len(t, lookup.Params, 2)
	assert.Equal(t, "key", lookup.Params[0].Name)
	assert.Equal(t, "string", lookup.Params[0].Info.goType)
	// A blank parameter is renamed so the wrapper can pass it on.
	assert.Equal(t, "a1", lookup.Params[1].Name)
	assert.Equal(t, kindHandle, lookup.Results[0].Info.kind)
	assert.Equal(t, "cppgo.Handle", lookup.Results[0].Info.goType)

	// Grouped results are flattened, and their names are kept.
	split := pkg.Funcs[4]
	require.Len(t, split.Results, 2)
	assert.Equal(t, "parts", split.Results[0].Name)
	assert.Equal(t, "err", split.Results[1].Name)
	assert.Equal(t, "(int32, error)", split.resultName())

	assert.Empty(t, pkg.Funcs[1].resultName())
	assert.Equal(t, "func Async()", pkg.Funcs[1].signature())
}

func TestParseBlockCommentDirectives(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": `package x

/*
cppgo:library libblock
*/

/*
Dot is documented with a block comment.

cppgo:external dot
*/
func Dot(a int32) int32
`,
	})
	pkg := mustParse(t, dir)
	assert.Equal(t, "libblock", pkg.Library)
	require.Len(t, pkg.Funcs, 1)
	assert.Equal(t, "dot", pkg.Funcs[0].Symbol)
	assert.Equal(t, []string{"// Dot is documented with a block comment."}, pkg.Funcs[0].Doc)
}

func TestParseDirectiveErrors(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
		// pos is set when the diagnostic points at a file:line.
		pos bool
	}{
		"missing library": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:external sym\nfunc F()\n"},
			want:  "missing //cppgo:library",
		},
		"empty library": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library\n\n//cppgo:external sym\nfunc F()\n"},
			want:  "needs a library name",
		},
		"library with spaces": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library lib x\n\n//cppgo:external s\nfunc F()\n"},
			want:  "single library name",
		},
		"duplicate library": {
			pos: true,
			files: map[string]string{
				"a.go": "package x\n\n//cppgo:library liba\n\n//cppgo:external s\nfunc F()\n",
				"b.go": "package x\n\n//cppgo:library libb\n",
			},
			want: "duplicate //cppgo:library",
		},
		"external not on a function": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external stray\nvar V int\n"},
			want:  "must be in the doc comment of a function declaration",
		},
		"external on a method": {
			pos: true,
			files: map[string]string{"x.go": `package x

//cppgo:library libx

type T struct{}

//cppgo:external meth
func (t T) Meth() {}
`},
			want: "cannot be used on a method",
		},
		"external on a generic function": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external gen\nfunc Gen[T any](v T) {}\n"},
			want:  "cannot be used on a generic function",
		},
		"empty symbol": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external\nfunc F()\n"},
			want:  "needs the C symbol name",
		},
		"invalid symbol": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external bad-name\nfunc F()\n"},
			want:  "must be a valid C identifier",
		},
		"duplicate symbol": {
			pos: true,
			files: map[string]string{"x.go": `package x

//cppgo:library libx

//cppgo:external same
func A()

//cppgo:external same
func B()
`},
			want: "duplicate symbol",
		},
		"duplicate function": {
			pos: true,
			files: map[string]string{
				"a.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external a\nfunc F()\n",
				"b.go": "package x\n\n//cppgo:external b\nfunc F()\n",
			},
			want: "duplicate function F",
		},
		"no externals": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\nfunc F() {}\n"},
			want:  "no //cppgo:external functions found",
		},
		"two externals on one function": {
			pos: true,
			files: map[string]string{"x.go": `package x

//cppgo:library libx

//cppgo:external one
//cppgo:external two
func F()
`},
			want: "duplicate //cppgo:external",
		},
		"variadic parameter": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F(v ...int32)\n"},
			want:  "variadic parameters are not supported",
		},
		"unsupported parameter": {
			pos:   true,
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F(v complex128)\n"},
			want:  "unsupported type complex128",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parsePackage(writePkg(t, tc.files))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			if tc.pos {
				// The diagnostic points at the offending directive.
				assert.Contains(t, err.Error(), ".go:")
			}
		})
	}
}

func TestParseDirectoryErrors(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		_, err := parsePackage(filepath.Join(t.TempDir(), "nope"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading package directory")
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.EqualError(t, err, "no Go files in "+dir)
	})

	t.Run("only generated files", func(t *testing.T) {
		dir := writePkg(t, map[string]string{
			"zz_cppgo.gen.go": banner + "package x\n",
		})
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "carry a \"Code generated\" banner")
	})

	t.Run("syntax error", func(t *testing.T) {
		dir := writePkg(t, map[string]string{"x.go": "package x\n\nfunc F( {\n"})
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "x.go:")
	})

	t.Run("mixed package names", func(t *testing.T) {
		dir := writePkg(t, map[string]string{
			"a.go": "package a\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F()\n",
			"b.go": "package b\n",
		})
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `found package "b"`)
	})

	t.Run("unreadable file", func(t *testing.T) {
		dir := writePkg(t, map[string]string{"x.go": "package x\n"})
		require.NoError(t, os.Chmod(filepath.Join(dir, "x.go"), 0o200))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "x.go"), 0o644) })
		if _, err := os.ReadFile(filepath.Join(dir, "x.go")); err == nil {
			t.Skip("the process can read write-only files (running as root?)")
		}
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading")
	})

	t.Run("diagnostic without a file name", func(t *testing.T) {
		err := posErr(token.Position{}, "boom")
		assert.EqualError(t, err, "boom")
	})
}

func TestParseGeneratedFileIsSkipped(t *testing.T) {
	// Generated files, _test.go files and files starting with _ or . are not
	// part of the package the generator binds.
	dir := writePkg(t, map[string]string{
		"real.go":    "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F()\n",
		"zz_gen.go":  banner + "package x\n\n//cppgo:library libother\n\n//cppgo:external other\nfunc G()\n",
		"x_test.go":  "package x\n\n//cppgo:external nope\nfunc T()\n",
		"_hidden.go": "package x\n\n//cppgo:library nope\n",
		"ignore.md":  "not go\n",
	})
	pkg := mustParse(t, dir)
	assert.Equal(t, "libx", pkg.Library)
	require.Len(t, pkg.Funcs, 1)
	assert.Equal(t, "F", pkg.Funcs[0].Name)
}

func TestParseSkipsDotFiles(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"real.go":     "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F()\n",
		".dotfile.go": "package x\n\n//cppgo:library other\n",
		"subdir/x.go": "package y\n",
	})
	pkg := mustParse(t, dir)
	assert.Equal(t, "libx", pkg.Library)
	require.Len(t, pkg.Files, 1)
}

func TestCommentHelpers(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, commentLines("// a\n// b"))
	assert.Equal(t, []string{"a", "b"}, commentLines("/* a\n * b\n */"))
	assert.Equal(t, []string{"a", ""}, commentLines("/* a\n\n */"))
	assert.Nil(t, commentLines("bare text"))
	assert.Nil(t, commentLines(""))
	assert.True(t, isDirectiveComment("//cppgo:external x"))
	assert.True(t, isDirectiveComment("/* cppgo:library y */"))
	assert.False(t, isDirectiveComment("// nope"))
	assert.Nil(t, directivesIn(nil, nil))
}

func TestTrimBlankComments(t *testing.T) {
	assert.Equal(t, []string{"// a"}, trimBlankComments([]string{"//", "// a", "//"}))
	assert.Empty(t, trimBlankComments([]string{"//", "//"}))
	assert.Empty(t, trimBlankComments(nil))
}

func TestPackageNameAndSourcesSurvive(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": "package weird\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F(a int32) int32\n",
	})
	pkg := mustParse(t, dir)
	assert.Equal(t, "weird", pkg.Name)
	require.Len(t, pkg.Files, 1)
	assert.NotEmpty(t, pkg.Files[0].src)
	assert.True(t, filepath.IsAbs(pkg.Files[0].Abs))
	assert.Equal(t, filepath.Join(dir, "x.go"), pkg.Files[0].Path)
	assert.Same(t, pkg.Funcs[0], pkg.byDecl[pkg.Funcs[0].decl])
}
