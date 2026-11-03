package gen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapperShapesAcrossResults renders one package per result shape, so the
// wrapper emitter is exercised on every branch: the zero values it returns on
// failure and the conversions it performs on success.
func TestWrapperShapesAcrossResults(t *testing.T) {
	files := renderMap(t, Config{Dir: writePkg(t, map[string]string{
		"x.go": `package x

//cppgo:library libx

//cppgo:external boolean
func Boolean() (bool, error)

//cppgo:external pointer
func Pointer() (*Point, error)

//cppgo:external handle
func Handle() (cppgo.Handle, error)

//cppgo:external make
func Make() *Point

//cppgo:external half
func Half(v float32) float32

//cppgo:external scale
func Scale(v float64) float64

//cppgo:external rune
func Rune() (rune, error)

//cppgo:external byte
func Byte() (byte, error)

// Point is declared here so the shim has a type to forward declare.
type Point struct{ X, Y float64 }
`,
	}), Out: t.TempDir()})
	src := files[GenFileName]

	// Zero values on the failure path.
	assert.Contains(t, src, "return false, err")
	assert.Contains(t, src, "return nil, err")
	assert.Contains(t, src, "return cppgo.Handle(0), err")

	// Pointer conversion on the success path.
	assert.Contains(t, src, "return (*Point)(cppgoRet), nil")
	assert.Contains(t, src, "return (*Point)(cppgoRet)")

	// byte and rune results widen to the C width and narrow on return.
	assert.Contains(t, src, "var cppgoRet int64")
	assert.Contains(t, src, "return rune(cppgoRet), nil")
	assert.Contains(t, src, "return byte(cppgoRet), nil")

	// Float results are stored through MOVSS/MOVSD, never through AX.
	asm := files[AsmFileName]
	assert.Contains(t, asm, "MOVSS\tX0, ret+")
	assert.Contains(t, asm, "MOVSD\tX0, ret+")
}

// TestLocalNameAvoidsParameters covers the shadowing guard.
func TestLocalNameAvoidsParameters(t *testing.T) {
	files := renderMap(t, Config{Dir: writePkg(t, map[string]string{
		"x.go": `package x

//cppgo:library libx

//cppgo:external f
func F(cppgoRet int32, cppgoStatus float64) (float64, error)
`,
	}), Out: t.TempDir()})
	src := files[GenFileName]
	assert.Contains(t, src, "var cppgoRet_ float64")
	assert.Contains(t, src, "cppgoStatus_ := cppgo_f(")
	// No argument points into Go memory, so there is nothing to keep alive.
	assert.NotContains(t, src, "cppgo.KeepAlive")
}

// TestShimForwardDeclaresResultTypes makes sure a pointer *result* also gets
// its opaque forward declaration.
func TestShimForwardDeclaresResultTypes(t *testing.T) {
	files := renderMap(t, Config{Dir: writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external make\nfunc Make() *Point\n\ntype Point struct{ X float64 }\n",
	}), Out: t.TempDir()})
	shim := files[ShimFileName]
	assert.Contains(t, shim, "struct Point;")
	assert.Contains(t, shim, "Point* cppgo_make(void);")
}

// TestImportPruning covers every import shape the rewriter has to reason about.
func TestImportPruning(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": `package x

import (
	"fmt"
	alias "os"
	_ "embed"
	. "strings"
)

//cppgo:library libx

//cppgo:external f
func F() error {
	fmt.Println(alias.Args, ToLower("X"))
	return nil
}
`,
	})
	src := renderMap(t, Config{Dir: dir, Out: t.TempDir()})[SourceFileName]

	// fmt and os were only used by the replaced body, so they are gone...
	assert.NotContains(t, src, `"fmt"`)
	assert.NotContains(t, src, `alias "os"`)
	// ...while the blank and dot imports, which cannot be analysed, stay.
	assert.Contains(t, src, `_ "embed"`)
	assert.Contains(t, src, `. "strings"`)
	assert.Contains(t, src, "func F() error { return cppgoCallF() }")
}

// TestImportDeclDroppedWhenEmpty removes the whole import declaration rather
// than leaving an empty one behind.
func TestImportDeclDroppedWhenEmpty(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": `package x

import "fmt"

//cppgo:library libx

//cppgo:external f
func F() {
	fmt.Println("replaced")
}
`,
	})
	src := renderMap(t, Config{Dir: dir, Out: t.TempDir()})[SourceFileName]
	assert.NotContains(t, src, "import")
	assert.NotContains(t, src, "fmt")
	assert.Contains(t, src, "func F() { cppgoCallF() }")
}

// TestUnnamedParameters covers a declaration without parameter names.
func TestUnnamedParameters(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external f\nfunc F(int32, string) int32\n",
	})
	files := renderMap(t, Config{Dir: dir, Out: t.TempDir()})
	src := files[SourceFileName]
	assert.Contains(t, src, "func F(a0 int32, a1 string) int32 { return cppgoCallF(a0, a1) }")
	assert.Contains(t, files[GenFileName], "func cppgoCallF(a0 int32, a1 string) int32")
}

// TestResultTypeErrorIsReported covers a bad type in the result list.
func TestResultTypeErrorIsReported(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external f\nfunc F() complex128\n",
	})
	_, err := parsePackage(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported type complex128")
	assert.Contains(t, err.Error(), ".go:")
}

// TestLibraryDirectiveInFunctionDoc covers a library directive that appears in
// a function's doc comment rather than on its own.
func TestLibraryDirectiveInFunctionDoc(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": `package x

// F does things.
//
//cppgo:library libx
//
//cppgo:external f
func F()
`,
	})
	pkg := mustParse(t, dir)
	assert.Equal(t, "libx", pkg.Library)
	require.Len(t, pkg.Funcs, 1)
	assert.Equal(t, []string{"// F does things."}, pkg.Funcs[0].Doc)
}

// TestGenerateReportsParseErrors checks the wrapper around Render.
func TestGenerateReportsParseErrors(t *testing.T) {
	err := Generate(Config{Dir: writePkg(t, map[string]string{"x.go": "package x\n"}), Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing //cppgo:library")
}

// TestEmitGoRejectsInvalidPackageName covers the formatter failing on the Go
// file, which no real input can produce.
func TestEmitGoRejectsInvalidPackageName(t *testing.T) {
	pkg := mustParse(t, writePkg(t, map[string]string{"x.go": validPkg}))
	pkg.Name = "1not a package"
	_, err := emitGo(pkg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "formatting generated Go source")
}

// TestEmitSourceRejectsCorruptSource covers the re-parse step: the rewrite is
// only accepted if the result still parses.
func TestEmitSourceRejectsCorruptSource(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	pkg := mustParse(t, dir)
	require.Len(t, pkg.Files, 1)
	// Appending keeps every recorded offset valid, so the failure is really
	// the re-parse of the rewritten text.
	pkg.Files[0].src = append([]byte(validPkg), []byte("\nfunc (\n")...)

	_, err := emitSource(pkg, pkg.Files[0], true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rewriting")
	assert.Contains(t, err.Error(), "x.go")
}

// TestGeneratedSourceIsStable checks the in-place rewrite never drifts.
func TestGeneratedSourceIsStable(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": validPkg})
	require.NoError(t, Generate(Config{Dir: dir}))
	first, err := readFileString(dir, "x.go")
	require.NoError(t, err)

	require.NoError(t, Generate(Config{Dir: dir}))
	second, err := readFileString(dir, "x.go")
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.True(t, strings.HasSuffix(first, "}\n"))
}

// TestModeDirective covers the calling-convention switch: shim is what this
// generator emits, and anything else must fail loudly instead of producing a
// binding that cannot work.
func TestModeDirective(t *testing.T) {
	t.Run("shim is accepted", func(t *testing.T) {
		dir := writePkg(t, map[string]string{
			"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:mode shim\n\n//cppgo:external f\nfunc F() int32\n",
		})
		pkg := mustParse(t, dir)
		assert.Equal(t, ModeShim, pkg.Mode)
		require.Len(t, pkg.Funcs, 1)
	})

	t.Run("abi requires a header", func(t *testing.T) {
		dir := writePkg(t, map[string]string{
			"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:mode abi\n\n//cppgo:external f\nfunc F() int32\n",
		})
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "//cppgo:mode abi needs //cppgo:header")
		assert.Contains(t, err.Error(), ".go:")
	})

	t.Run("unknown mode is rejected", func(t *testing.T) {
		dir := writePkg(t, map[string]string{
			"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:mode direct\n\n//cppgo:external f\nfunc F() int32\n",
		})
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unsupported //cppgo:mode "direct"`)
		assert.Contains(t, err.Error(), `"abi"`)
		assert.Contains(t, err.Error(), ".go:")
	})

	t.Run("duplicate mode is rejected", func(t *testing.T) {
		dir := writePkg(t, map[string]string{
			"a.go": "package x\n\n//cppgo:library libx\n\n//cppgo:mode shim\n\n//cppgo:external f\nfunc F() int32\n",
			"b.go": "package x\n\n//cppgo:mode shim\n",
		})
		_, err := parsePackage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate //cppgo:mode")
	})
}
