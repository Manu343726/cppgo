package gen

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedGoShape asserts the API shapes DESIGN.md specifies: an opaque
// value with a Go-side object, a mirrored POD with compile-time layout
// assertions, a direct call with no error, and a thunk with one.
func TestGeneratedGoShape(t *testing.T) {
	gen := renderMap(t, mathxConfig())[GenFileName]

	for _, want := range []string{
		"// Point is mathx::Point: a POD, 16 bytes, X at offset 0, Y at offset 8.",
		"type Point struct {\n\tX float64\n\tY float64\n}",
		"var _ [16]byte = [unsafe.Sizeof(Point{})]byte{}",
		"var _ [8]byte = [unsafe.Offsetof(Point{}.Y)]byte{}",
		"// Vector is mathx::Vector. The object belongs to C++; obj is where it lives.",
		"type Vector struct {\n\tobj *cppgo.Object\n}",
		"func (v *Vector) cppgoAddr() uintptr",
		"func NewVector(",
		"cppgo.NewObject(16, 8, func() {",
		"constructed := false",
		"func (v *Vector) Clone() (*Vector, error)",
		"func (v *Vector) Close() error",
		"obj := v.obj\n\tv.obj = nil\n\tobj.Release()",
		"func (v *Vector) Norm() (float64, error)",
		"func (v *Vector) Scale(k float64)",
		"func (v *Vector) Dot(ys []float64) (float64, error)",
		"func Saxpy(a float64, x []float64, y []float64) error",
		"func VectorDims() (int32, error)",
		`if len(x) != len(y) {
		return fmt.Errorf("cppgo: mathx::saxpy: the slices must have the same length, got %d and %d", len(x), len(y))
	}`,
		"// C++: mathx::Vector::scale(double) noexcept, _ZN5mathx6Vector5scaleEd (direct, noexcept, out of line, not virtual, register class)",
		"// C++: mathx::Vector::norm() const, _ZNK5mathx6Vector4normEv (thunk cppgo_thunk_",
		"&cppgo.CallError{Symbol: symbol, Err: err}",
		"func Open(path string) error { return cppgoOpen(path) }",
		"cppgoShimName = \"libcppgo_mathx.so\"",
		"cppgoUserLibrary = \"libmathx\"",
	} {
		assert.Contains(t, gen, want)
	}
	assert.NotContains(t, gen, "cppgo.Handle", "generated code must not expose Handle")
}

// TestGeneratedAsmShape asserts the trampoline's non-negotiable rules: the
// arguments are read first, SP is switched and restored, and only then is the
// result stored.
func TestGeneratedAsmShape(t *testing.T) {
	asm := renderMap(t, mathxConfig())[AsmFileName]

	for _, want := range []string{
		"TEXT ·cppgoCall_",
		"NOSPLIT, $0-",
		"MOVQ\tfn+0(FP), R12",
		"MOVQ\tSP, R13",
		"ANDQ\t$~15, SP",
		"CALL\tR12",
		"MOVQ\tR13, SP",
		"MOVL\tAX, ret+",
		"MOVQ\tthis+8(FP), DI",
	} {
		assert.Contains(t, asm, want)
	}
}

// TestTypeFixtureAsmShape checks the float and integer result slots the wider
// type fixture produces.
func TestTypeFixtureAsmShape(t *testing.T) {
	asm := renderMap(t, fixtureConfig("types", "types.hpp"))[AsmFileName]
	for _, want := range []string{
		"MOVSD\tX0, ret+",
		"MOVSS\tX0, ret+",
		"MOVL\tAX, ret+",
		"MOVQ\tAX, ret+",
	} {
		assert.Contains(t, asm, want)
	}
}

// TestGeneratedShimShape asserts that the thunk calls as C++, catches, and
// clears the thread-local message on entry.
func TestGeneratedShimShape(t *testing.T) {
	files := renderMap(t, mathxConfig())
	header, source := files[ShimHeaderFileName], files[ShimSourceFileName]

	for _, want := range []string{
		"extern \"C\" {",
		"const char *cppgo_last_error(void);",
		"int32_t cppgo_thunk_",
		"void * self",
		"const void * self",
	} {
		assert.Contains(t, header, want)
	}
	for _, want := range []string{
		"thread_local std::string cppgo_error_message;",
		"extern \"C\" const char *cppgo_last_error(void)",
		"cppgo_error_message.clear();",
		"try {",
		"catch (const std::exception &error)",
		"catch (...)",
		"new (self) ::mathx::Vector(x, y);",
		"static_cast<const ::mathx::Vector *>(self)->dot(ys, n);",
		"static_cast<::mathx::Vector *>(self)->~Vector();",
		"::mathx::saxpy(a, x, y, n);",
		"*cppgo_result =",
	} {
		assert.Contains(t, source, want)
	}
}

// TestTypeFixtureEmission checks the shapes the type fixture adds: direct
// scalar calls with no error, the bool helper, a storage-only Close, and a
// noexcept constructor with no status.
func TestTypeFixtureEmission(t *testing.T) {
	gen := renderMap(t, fixtureConfig("types", "types.hpp"))[GenFileName]

	for _, want := range []string{
		"func AddI8(a int8, b int8) int8",
		"func AddU64(a uint64, b uint64) uint64",
		"func AddF32(a float32, b float32) float32",
		"func Negate(value bool) bool",
		"func PointerIdentity(value *int32) *int32",
		"func Count(values []int32) uint32",
		"func Read(value *Opaque) int32",
		"func SumPod(pod *Pod) float64",
		"func AddInto(a []int32, b []int32) {",
		"panic(fmt.Sprintf(\"cppgo: types::add_into: the slices must have the same length, got %d and %d\", len(a), len(b)))",
		"func cppgoBool(v bool) int64",
		"func (n *NoDtor) Close() error",
		"obj := n.obj\n\tn.obj = nil\n\tobj.Release()\n\treturn nil",
		"func NewNoDtor(",
		"func NewEmpty() (*Empty, error)",
		"func NewImplicit() (*Implicit, error)",
		"cppgo.NewObject(4, 4, nil)",
	} {
		assert.Contains(t, gen, want)
	}
}

// TestOverloadEmission checks the generated names of the overload fixture.
func TestOverloadEmission(t *testing.T) {
	gen := renderMap(t, fixtureConfig("overloads", "overloads.hpp"))[GenFileName]
	for _, want := range []string{
		"func NewThing() (*Thing, error)",
		"func NewThingInt32(value int32) (*Thing, error)",
		"func (t *Thing) Value() (int32, error)",
		"func (t *Thing) ValueInt32(scale int32) (int32, error)",
		"func (t *Thing) Reset() error",
		"func (t *Thing) ResetInt32(value int32) error",
		"func AddInt32Int32(a int32, b int32) int32",
		"func AddFloat64Float64(a float64, b float64) float64",
	} {
		assert.Contains(t, gen, want)
	}
}

// TestEveryBindingHasACarrier is the contract's coverage test: every binding in
// every fixture resolved to exactly one carrier, with a reason.
func TestEveryBindingHasACarrier(t *testing.T) {
	for name, cfg := range goldenFixtures() {
		model, err := build(cfg)
		require.NoError(t, err, name)
		require.NotEmpty(t, model.bindings, name)
		for _, binding := range model.bindings {
			assert.NotEmpty(t, binding.Reason, "%s: %s", name, binding.sym.QualifiedName)
			assert.NotEmpty(t, binding.Symbol, "%s: %s", name, binding.sym.QualifiedName)
			assert.NotEmpty(t, binding.Trampoline, "%s: %s", name, binding.sym.QualifiedName)
			if binding.Carrier.shimCarried() {
				assert.NotEmpty(t, binding.Thunk)
			} else {
				assert.Empty(t, binding.Thunk)
				assert.NotEmpty(t, binding.sym.Mangled)
			}
		}
	}
}

// TestFixtureShimsCompile compiles the generated shim of each fixture with a
// real C++ compiler. The mathx fixture is linked and run end to end elsewhere;
// this is the syntax proof for the wider type and overload shapes.
func TestFixtureShimsCompile(t *testing.T) {
	compiler, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	for name, cfg := range goldenFixtures() {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := t.TempDir()
			cfg.Out = out
			require.NoError(t, Generate(cfg))
			cwd, err := os.Getwd()
			require.NoError(t, err)
			standard := "-std=c++17"
			if name == "std" {
				// std::span is C++20; the fixture's .clangd says so too.
				standard = "-std=c++20"
			}
			cmd := exec.Command(compiler, standard, "-fsyntax-only", "-I"+cwd, ShimPath(out, ShimSourceFileName))
			cmd.Dir = cwd
			output, err := cmd.CombinedOutput()
			require.NoErrorf(t, err, "%s rejected %s's shim:\n%s", compiler, name, output)
		})
	}
}

// TestStaleShimIsNotWrittenInCheckMode is a small guard on the writer: check
// mode must never create the output directory.
func TestStaleShimIsNotWrittenInCheckMode(t *testing.T) {
	out := filepath.Join(t.TempDir(), "never")
	cfg := mathxConfig()
	cfg.Out = out
	cfg.Check = true
	err := Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of date")
	_, statErr := os.Stat(out)
	assert.True(t, os.IsNotExist(statErr), "check mode must not create the output directory")
}

// TestGeneratedGoFilesAreGofmtClean checks that the generated Go is exactly
// what gofmt produces, which the emitter guarantees by formatting it.
func TestGeneratedGoFilesAreGofmtClean(t *testing.T) {
	for name, cfg := range goldenFixtures() {
		gen := renderMap(t, cfg)[GenFileName]
		formatted, err := format.Source([]byte(gen))
		require.NoError(t, err, name)
		assert.Equal(t, gen, string(formatted), name)
	}
}
