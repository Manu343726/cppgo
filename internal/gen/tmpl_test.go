package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tmplConfig is the template fixture with both explicit instantiations named.
func tmplConfig() Config {
	cfg := fixtureConfig("tmpl", "cell.hpp")
	cfg.Instantiate = []string{"tmpl::Cell<int>", "tmpl::Cell<double>"}
	return cfg
}

// TestInstantiateModel checks that -instantiate selects the concrete records
// and that their pattern members are bound with the substituted signatures.
func TestInstantiateModel(t *testing.T) {
	model, err := build(tmplConfig())
	require.NoError(t, err)

	intCell := findRecord(t, model, "CellInt")
	assert.False(t, intCell.Mirrored, "a user constructor and destructor make it opaque")
	assert.Equal(t, "tmpl::Cell<int>", intCell.CxxName)
	require.NotNil(t, intCell.Dtor)
	require.Len(t, intCell.Ctors, 1)
	assert.Equal(t, "value", intCell.Ctors[0].Params[0].Name)
	assert.Equal(t, "int32", intCell.Ctors[0].Params[0].Type.Go)

	doubleCell := findRecord(t, model, "CellFloat64")
	assert.Equal(t, "tmpl::Cell<double>", doubleCell.CxxName)

	findBinding(t, model, "NewCellInt")
	findBinding(t, model, "NewCellFloat64")
	var get *binding
	for _, candidate := range model.bindings {
		if candidate.GoName == "Get" && candidate.Owner != nil && candidate.Owner.GoName == "CellInt" {
			get = candidate
		}
	}
	require.NotNil(t, get)
	assert.Equal(t, "int32", get.Result.Go)
}

// TestInstantiateOutput checks the generated Go names and the shim's use of the
// instantiated type spelling.
func TestInstantiateOutput(t *testing.T) {
	files := renderMap(t, tmplConfig())
	gen := files[GenFileName]
	for _, want := range []string{
		"type CellInt struct {\n\tobj *cppgo.Object\n\t// view is the address of this value inside the object, 0 for the object itself.\n\taddr uintptr\n}",
		"type CellFloat64 struct {\n\tobj *cppgo.Object\n\t// view is the address of this value inside the object, 0 for the object itself.\n\taddr uintptr\n}",
		"func NewCellInt(value int32) (*CellInt, error)",
		"func NewCellFloat64(value float64) (*CellFloat64, error)",
		"func (c *CellInt) Get() (int32, error)",
		"func (c *CellFloat64) Set(value float64) error",
	} {
		assert.Contains(t, gen, want)
	}
	shim := files[ShimSourceFileName]
	assert.Contains(t, shim, "new (self) ::tmpl::Cell<int>(value)")
	assert.Contains(t, shim, "static_cast<::tmpl::Cell<int> *>(self)->~Cell()")
	assert.Contains(t, shim, "static_cast<const ::tmpl::Cell<double> *>(self)->get()")
}

// TestInstantiateUnmatched checks that a typo in -instantiate cannot silently
// emit nothing.
func TestInstantiateUnmatched(t *testing.T) {
	cfg := fixtureConfig("tmpl", "cell.hpp")
	cfg.Instantiate = []string{"tmpl::Cell<float>"}
	_, err := build(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-instantiate tmpl::Cell<float>")
	assert.Contains(t, err.Error(), "names no instantiation")
}

// TestTemplateGenericAPI checks phase one: a class template becomes a generic
// Go type whose constructors and methods substitute the type parameter, and the
// package compiles before any concrete instantiation exists.
func TestTemplateGenericAPI(t *testing.T) {
	model, err := build(fixtureConfig("tmpl", "cell.hpp"))
	require.NoError(t, err)
	require.Len(t, model.templates, 1)
	tmpl := model.templates[0]
	assert.Equal(t, "Cell", tmpl.GoName)
	assert.Equal(t, "tmpl::Cell", tmpl.CxxName)
	require.Len(t, tmpl.Ctors, 1)
	require.Len(t, tmpl.Methods, 2)
	require.NotNil(t, tmpl.Dtor)
	assert.Empty(t, model.instantiations, "phase one knows no concrete type")

	files := renderMap(t, fixtureConfig("tmpl", "cell.hpp"))
	gen := files[GenFileName]
	for _, want := range []string{
		"type Cell[T any] struct {",
		"func NewCell[T any](value T) (*Cell[T], error)",
		"func (c *Cell[T]) Close() error",
		"func (c *Cell[T]) Get() (T, error)",
		"func (c *Cell[T]) Set(value T) error",
	} {
		assert.Contains(t, gen, want)
	}
	assert.NotContains(t, files[ShimSourceFileName], "template class ::tmpl::Cell")
}

// TestInstantiateShimCompiles compiles the generated shim and links it with the
// fixture's translation unit, which is where the explicit instantiations live.
func TestInstantiateShimCompiles(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	out := t.TempDir()
	cfg := tmplConfig()
	cfg.Out = out
	require.NoError(t, Generate(cfg))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	library := filepath.Join(out, "libtmpl.so")
	args := []string{"-std=c++17", "-O1", "-fPIC", "-shared", "-I" + cwd,
		filepath.Join(cwd, "testdata", "tmpl", "cell.cpp"),
		ShimPath(out, ShimSourceFileName), "-o", library}
	output, err := exec.Command(gxx, args...).CombinedOutput()
	require.NoErrorf(t, err, "g++ rejected the template shim:\n%s", output)

	exported, err := exec.Command("nm", "-D", "--defined-only", library).Output()
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(exported), "cppgo_thunk_"), "the shim's thunks must be exported")
}

// TestStaticAssertingMemberDoesNotRefuseTheInstantiation is #112's Eigen wall:
// `wall::Strict<T>` has a member that only compiles for one-byte T (the shape
// of Eigen's THIS_METHOD_IS_ONLY_FOR_VECTORS_OF_A_SPECIFIC_SIZE). The generic
// API skips the operator, and the shim no longer writes a class-level
// `template class`, so instantiating Strict<int32> writes only the members the
// API calls and the translation unit compiles.
func TestStaticAssertingMemberDoesNotRefuseTheInstantiation(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	out := t.TempDir()
	cfg := fixtureConfig("tmpl", "wall.hpp")
	cfg.Package = "wall"
	cfg.Library = "libwall"
	cfg.Out = out
	cfg.Bind = []string{"wall::Strict"}
	cfg.Instantiation = []Instantiation{{Template: "wall::Strict", Args: []string{"int32"}}}
	require.NoError(t, Generate(cfg))

	shim := readFileString(t, ShimPath(out, ShimSourceFileName))
	assert.NotContains(t, shim, "template class ::wall::Strict")
	assert.Contains(t, shim, "return sizeof(::wall::Strict<std::int32_t>);")
	output, err := exec.Command(gxx, "-std=c++17", "-fsyntax-only", "-I.",
		ShimPath(out, ShimSourceFileName)).CombinedOutput()
	require.NoErrorf(t, err, "g++ rejected the shim:\n%s", output)
}
