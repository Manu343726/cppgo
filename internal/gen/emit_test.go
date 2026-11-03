package gen

import (
	"flag"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update rewrites the golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// goldenDir is the input/output pair of one golden case.
func goldenDirs(name string) (in, out string) {
	base := filepath.Join("testdata", "golden", name)
	return filepath.Join(base, "in"), filepath.Join(base, "out")
}

// TestGolden compares every generated file with the checked-in copy.
func TestGolden(t *testing.T) {
	for _, name := range []string{"basic", "abi"} {
		t.Run(name, func(t *testing.T) {
			if name != "basic" && !libclangAvailable() {
				t.Skip("libclang is not available: the direct ABI mode cannot resolve its bindings")
			}
			in, out := goldenDirs(name)
			renderInto(t, in, out)
		})
	}
}

// renderInto renders and compares (or updates) a golden directory.
func renderInto(t *testing.T, in, out string) {
	t.Helper()
	outs, err := Render(Config{Dir: in, Out: out})
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.MkdirAll(out, 0o755))
		for _, o := range outs {
			require.NoError(t, os.WriteFile(filepath.Join(out, o.Name), o.Data, 0o644))
		}
	}

	for _, o := range outs {
		want, err := os.ReadFile(filepath.Join(out, o.Name))
		require.NoError(t, err, "missing golden file %s (run go test -update)", o.Name)
		assert.Equal(t, string(want), string(o.Data), o.Name)
	}

	// The golden directory must not keep files the generator no longer emits.
	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	var have []string
	for _, e := range entries {
		have = append(have, e.Name())
	}
	var want []string
	for _, o := range outs {
		want = append(want, o.Name)
	}
	sort.Strings(have)
	assert.Equal(t, want, have)
}

// TestGoldenCoversBothSources checks the fixture really exercises the paths it
// is supposed to: a second input file, a replaced body and a pruned import.
func TestGoldenCoversBothSources(t *testing.T) {
	files := renderMap(t, Config{Dir: "testdata/golden/basic/in", Out: "testdata/golden/basic/out"})
	require.Contains(t, files, SourceFileName)

	main := files[SourceFileName]
	assert.Contains(t, main, "func AddI64(a, b int64) int64 { return cppgoCallAddI64(a, b) }")
	assert.Contains(t, main, "//cppgo:external add_i64")
	assert.Contains(t, main, "type Point struct")

	second := files["zz_cppgo_stub_more.go"]
	// The body was replaced, and the import it was the only user of is gone.
	assert.Contains(t, second, "func Boom(code int32) error { return cppgoCallBoom(code) }")
	assert.NotContains(t, second, "fmt")
	assert.Contains(t, second, "strings.ToLower")
	assert.Contains(t, second, "func Lower(s string) string {")
}

// TestOutputIsValidGo parses and formats every .go output, which is the
// strongest cheap guarantee that the generator emits legal Go.
func TestOutputIsValidGo(t *testing.T) {
	for _, name := range []string{"basic", "abi"} {
		t.Run(name, func(t *testing.T) {
			if name != "basic" && !libclangAvailable() {
				t.Skip("libclang is not available: the direct ABI mode cannot resolve its bindings")
			}
			files := renderMap(t, Config{Dir: filepath.Join("testdata", "golden", name, "in"), Out: t.TempDir()})
			for file, data := range files {
				if !strings.HasSuffix(file, ".go") {
					continue
				}
				fset := token.NewFileSet()
				_, err := parser.ParseFile(fset, file, data, parser.ParseComments)
				require.NoError(t, err, file)

				formatted, err := format.Source([]byte(data))
				require.NoError(t, err, file)
				assert.Equal(t, string(formatted), data, "%s is not gofmt'd", file)
			}
		})
	}
}

// TestOutputIsDeterministic renders the same package twice.
func TestOutputIsDeterministic(t *testing.T) {
	cfg := Config{Dir: "testdata/golden/basic/in", Out: "testdata/golden/basic/out"}
	first := renderMap(t, cfg)
	second := renderMap(t, cfg)
	assert.Equal(t, first, second)

	// The order of the files on disk must not matter either: the model is
	// sorted by name.
	dir := writePkg(t, map[string]string{
		"b.go": "package x\n\n//cppgo:external zeta\nfunc Zeta()\n",
		"a.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external alpha\nfunc Alpha()\n",
		"c.go": "package x\n\n//cppgo:external middle\nfunc Middle()\n",
	})
	out := t.TempDir()
	files := renderMap(t, Config{Dir: dir, Out: out})
	gen := files[GenFileName]
	assert.Less(t, strings.Index(gen, "cppgoCallAlpha"), strings.Index(gen, "cppgoCallMiddle"))
	assert.Less(t, strings.Index(gen, "cppgoCallMiddle"), strings.Index(gen, "cppgoCallZeta"))
}

// TestTrampolineShape pins the non-negotiable parts of the assembly: arguments
// are read before SP is switched, the stack is aligned before the call, and
// the result is stored after SP is restored.
func TestTrampolineShape(t *testing.T) {
	files := renderMap(t, Config{Dir: "testdata/golden/basic/in", Out: "testdata/golden/basic/out"})
	asm := files[AsmFileName]

	assert.Contains(t, asm, "#include \"textflag.h\"")
	assert.Contains(t, asm, "TEXT ·cppgo_add_i64(SB), NOSPLIT, $0-32")

	add := asmBody(t, asm, "TEXT ·cppgo_add_i64(SB)")
	require.NotEmpty(t, add)
	assert.Equal(t, []string{
		"MOVQ\tfn+0(FP), R12",
		"MOVQ\ta0+8(FP), DI",
		"MOVQ\ta1+16(FP), SI",
		"MOVQ\tSP, R13",
		"ANDQ\t$~15, SP",
		"CALL\tR12",
		"MOVQ\tR13, SP",
		"MOVQ\tAX, ret+24(FP)",
		"RET",
	}, add)

	// Floats go through XMM registers, in their own order.
	move := asmBody(t, asm, "TEXT ·cppgo_move(SB)")
	assert.Contains(t, move, "MOVQ\ta0+8(FP), DI")
	assert.Contains(t, move, "MOVSD\ta1+16(FP), X0")
	assert.Contains(t, move, "MOVSD\ta2+24(FP), X1")
	assert.Contains(t, move, "RET")

	// A function with no result has no store at all.
	init := asmBody(t, asm, "TEXT ·cppgo_init(SB)")
	assert.Equal(t, "RET", init[len(init)-1])
	assert.NotContains(t, strings.Join(init, "\n"), "ret+")

	// The status comes back in the low 32 bits.
	status := asmBody(t, asm, "TEXT ·cppgo_status(SB)")
	assert.Equal(t, "MOVL\tAX, ret+8(FP)", status[len(status)-2])
}

// asmBody returns the instruction lines of one TEXT block.
func asmBody(t *testing.T, asm, header string) []string {
	t.Helper()
	idx := strings.Index(asm, header)
	require.GreaterOrEqual(t, idx, 0, header)
	var out []string
	for _, line := range strings.Split(asm[idx:], "\n")[1:] {
		if line == "" || strings.HasPrefix(line, "//") {
			if len(out) > 0 {
				break
			}
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		out = append(out, strings.TrimPrefix(strings.TrimSuffix(line, "\r"), "\t"))
	}
	return out
}

// TestShimShape checks the C++ side of the contract.
func TestShimShape(t *testing.T) {
	files := renderMap(t, Config{Dir: "testdata/golden/basic/in", Out: "testdata/golden/basic/out"})
	shim := files[ShimFileName]

	assert.Contains(t, shim, "#ifndef CPPGO_MATHX_SHIM_H")
	assert.Contains(t, shim, "#define CPPGO_MATHX_SHIM_H")
	assert.Contains(t, shim, "#endif /* CPPGO_MATHX_SHIM_H */")
	assert.Contains(t, shim, "#include <stddef.h>")
	assert.Contains(t, shim, "#include <stdint.h>")
	assert.Contains(t, shim, "extern \"C\" {")
	assert.Contains(t, shim, "const char *cppgo_last_error(void);")

	// Plain value returns.
	assert.Contains(t, shim, "int64_t cppgo_add_i64(int64_t a, int64_t b);")
	assert.Contains(t, shim, "void cppgo_init(const char* name, size_t name_len);")
	assert.Contains(t, shim, "void* cppgo_lookup(const char* key, size_t key_len, int32_t kind);")
	assert.Contains(t, shim, "void cppgo_move(Point* p, double dx, double dy);")

	// A status return, with the value through an out-pointer.
	assert.Contains(t, shim, "int32_t cppgo_dot(double* a, size_t a_len, double* b, size_t b_len, double* ret);")
	assert.Contains(t, shim, "int32_t cppgo_status(void);")

	// The bound package's own types are forward-declared.
	assert.Contains(t, shim, "struct Point;")
	assert.Contains(t, shim, "typedef struct Point Point;")

	// The error convention is documented where the C++ side will read it.
	assert.Contains(t, shim, "cppgo_last_error()")
	assert.Contains(t, shim, "must not keep a pointer to Go memory")
}

// TestGoWrapperShapes checks the shape of each kind of wrapper.
func TestGoWrapperShapes(t *testing.T) {
	files := renderMap(t, Config{Dir: "testdata/golden/basic/in", Out: "testdata/golden/basic/out"})
	src := files[GenFileName]

	// Library plumbing.
	assert.Contains(t, src, "const cppgoLibraryName = \"libmathx\"")
	assert.Contains(t, src, "cppgoOnce sync.Once")
	assert.Contains(t, src, "{name: \"cppgo_add_i64\", addr: &cppgoSymAddI64},")
	assert.Contains(t, src, "lib, err := cppgo.Open(path)")

	// A function that cannot fail panics when the library is missing.
	assert.Contains(t, src, `func cppgoCallAddI64(a int64, b int64) int64 {
	if err := cppgoEnsure(); err != nil {
		panic(err)
	}
	cppgo.ProbeStack()
	return cppgo_add_i64(cppgoSymAddI64, a, b)
}`)

	// A slice expands into (pointer, length) and is kept alive.
	assert.Contains(t, src, "cppgo_dot(cppgoSymDot, cppgo.Ptr(a), uintptr(len(a)), cppgo.Ptr(b), uintptr(len(b)), unsafe.Pointer(&cppgoRet))")
	assert.Contains(t, src, "cppgo.KeepAlive(a, b)")

	// A status return is turned into an error naming the symbol.
	assert.Contains(t, src, `if cppgoStatus != 0 {
		if err := cppgo.LastError(cppgoLib); err != nil {
			return 0, fmt.Errorf("dot: %w", err)
		}
		return 0, fmt.Errorf("dot: call failed with status %d", cppgoStatus)
	}`)

	// bool arguments go through the int32_t conversion helper.
	assert.Contains(t, src, "func cppgoBoolInt64(v bool) int64 {")
	assert.Contains(t, src, "cppgoBoolInt64(flag)")

	// The documentation of the binding is carried over.
	assert.Contains(t, src, "// AddI64 adds two integers.")
}

// TestBoolHelperIsConditional keeps the generated file free of unused helpers.
func TestBoolHelperIsConditional(t *testing.T) {
	withBool := renderMap(t, Config{Dir: writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F(v bool) bool\n",
	}), Out: t.TempDir()})
	assert.Contains(t, withBool[GenFileName], "cppgoBoolInt64")

	withoutBool := renderMap(t, Config{Dir: writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F(v int32) int32\n",
	}), Out: t.TempDir()})
	assert.NotContains(t, withoutBool[GenFileName], "cppgoBoolInt64")
	// No pointer ever crosses, so unsafe is not imported.
	assert.NotContains(t, withoutBool[GenFileName], "\"unsafe\"")
}

// TestSourceFiles covers both source-declaration shapes: a body that is
// replaced, and a declaration with no body at all.
func TestSourceFiles(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": `package x

//cppgo:library libx

//cppgo:external a
func A(v int32) int32 { return 1 }

//cppgo:external b
func B()
`,
	})
	files := renderMap(t, Config{Dir: dir, Out: t.TempDir()})
	src := files[SourceFileName]
	assert.Contains(t, src, "func A(v int32) int32 { return cppgoCallA(v) }")
	assert.Contains(t, src, "func B() { cppgoCallB() }")
	assert.NotContains(t, src, "return 1")
}

// TestInPlaceRenamesNothing checks that generating into the package directory
// rewrites the input files instead of emitting a copy next to them.
func TestInPlaceRenamesNothing(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:external s\nfunc F(v int32) int32\n",
	})
	require.NoError(t, Generate(Config{Dir: dir, Out: dir}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{ShimFileName, "x.go", GenFileName, AsmFileName}, names)

	// The rewritten file still carries the directives, so a second run is a
	// no-op rather than a wipe.
	data, err := os.ReadFile(filepath.Join(dir, "x.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "//cppgo:external s")
	assert.Contains(t, string(data), "return cppgoCallF(v)")
	assert.NotContains(t, string(data), GeneratedBanner)
}
