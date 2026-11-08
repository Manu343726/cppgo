package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestStdFixtureShape checks the Go API DESIGN.md's type map specifies for
// every standard-library row.
func TestStdFixtureShape(t *testing.T) {
	files := renderMap(t, fixtureConfig("std", "std.hpp"))
	gen := files[GenFileName]
	for _, want := range []string{
		"func Describe(prefix string, suffix string) (string, error)",
		"func TextLength(value string) (uint64, error)",
		"func LiteralView() (string, error)",
		"func Doubled(values []float64) ([]float64, error)",
		"func Sum(values []float64) (float64, error)",
		"func Triple(values [3]float64) ([3]float64, error)",
		"func Maybe(present bool, value float64) (*float64, error)",
		"func Nothing() ([]int32, error)",
		"func ScaleAll(values []float64, factor float64) error",
		"func Unwrap(value *float64, fallback float64) (float64, error)",
	} {
		assert.Contains(t, gen, want)
	}
	shim := files[ShimSourceFileName]
	assert.Contains(t, shim, "std::string cppgo_prefix(")
	assert.Contains(t, shim, "std::string_view cppgo_suffix(")
	assert.Contains(t, shim, "std::vector<double> cppgo_values;")
	assert.Contains(t, shim, "std::span<const double> cppgo_values(")
	assert.Contains(t, shim, "std::array<double, 3> cppgo_values{};")
	assert.Contains(t, shim, "std::optional<double> cppgo_value;")
	assert.Contains(t, shim, "::operator new[]")
	assert.Contains(t, shim, "cppgo_free")
}

// TestStdFixtureEndToEnd builds the library, compiles the shim into it, and
// calls every standard-library binding through the generated package.
func TestStdFixtureEndToEnd(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not in PATH")
	}
	if _, err := cxx.Load(); err != nil {
		t.Skipf("libclang is not available: %v", err)
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/std")
	require.NoError(t, err)

	tmp := t.TempDir()
	cxxDir := filepath.Join(tmp, "cxx")
	require.NoError(t, copyTree(fixture, cxxDir))
	lib := filepath.Join(tmp, "libstdx.so")
	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "genpkg")

	header := filepath.Join(cxxDir, "std.hpp")
	require.NoError(t, Generate(Config{
		Headers: []string{header},
		Package: "stdx",
		Library: lib,
		Out:     out,
	}))
	var args = []string{"-std=c++20", "-O1", "-fPIC", "-shared", "-I" + cxxDir,
		filepath.Join(cxxDir, "std.cpp"), filepath.Join(out, ShimSourceFileName), "-o", lib}
	output, err := exec.Command(gxx, args...).CombinedOutput()
	require.NoErrorf(t, err, "g++ failed:\n%s", output)

	for _, compiler := range []string{"g++", "clang++"} {
		path, err := exec.LookPath(compiler)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, "-std=c++20", "-fsyntax-only", "-I"+cxxDir, filepath.Join(out, ShimSourceFileName))
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "%s rejected the std shim:\n%s", compiler, output)
	}

	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "std_test.go"), []byte(stdTestSource), 0o644))
	runGo(t, goBin, moduleDir, "vet", "./genpkg")
	ran := runGoEnv(t, goBin, moduleDir, []string{"CPPGO_STD_LIB=" + lib}, "test", "-count=1", "-v", "./genpkg")
	assert.Contains(t, ran, "--- PASS: TestStd")
}

// stdTestSource is the test the generated std package runs.
const stdTestSource = `package stdx

import (
	"math"
	"os"
	"testing"
)

func TestStd(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_STD_LIB")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	text, err := Describe("pre", "fix")
	if err != nil || text != "pre:fix" {
		t.Fatalf("Describe = %q, %v", text, err)
	}
	if text, err := Describe("", ""); err != nil || text != ":" {
		t.Fatalf("Describe empty = %q, %v", text, err)
	}
	if n, err := TextLength("hello"); err != nil || n != 5 {
		t.Fatalf("TextLength = %d, %v", n, err)
	}
	if view, err := LiteralView(); err != nil || view != "view" {
		t.Fatalf("LiteralView = %q, %v", view, err)
	}

	doubled, err := Doubled([]float64{1, 2, 3})
	if err != nil {
		t.Fatalf("Doubled: %v", err)
	}
	if len(doubled) != 3 || doubled[0] != 2 || doubled[1] != 4 || doubled[2] != 6 {
		t.Fatalf("Doubled = %v", doubled)
	}
	if empty, err := Doubled(nil); err != nil || len(empty) != 0 {
		t.Fatalf("Doubled(nil) = %v, %v", empty, err)
	}
	if got, err := Unwrap(nil, 7); err != nil || got != 7 {
		t.Fatalf("Unwrap(nil) = %v, %v", got, err)
	}
	if three := 3.5; true {
		if got, err := Unwrap(&three, 7); err != nil || got != 3.5 {
			t.Fatalf("Unwrap(3.5) = %v, %v", got, err)
		}
	}
	if n, err := Nothing(); err != nil || len(n) != 0 {
		t.Fatalf("Nothing = %v, %v", n, err)
	}

	borrowed := []float64{1, 2, 3}
	if err := ScaleAll(borrowed, 2); err != nil {
		t.Fatalf("ScaleAll: %v", err)
	}
	if borrowed[0] != 2 || borrowed[1] != 4 || borrowed[2] != 6 {
		t.Fatalf("ScaleAll did not write through the borrowed slice: %v", borrowed)
	}

	if total, err := Sum([]float64{1, 2, 3, 4}); err != nil || total != 10 {
		t.Fatalf("Sum = %v, %v", total, err)
	}

	tripled, err := Triple([3]float64{1, 2, 3})
	if err != nil {
		t.Fatalf("Triple: %v", err)
	}
	if tripled != [3]float64{3, 6, 9} {
		t.Fatalf("Triple = %v", tripled)
	}

	missing, err := Maybe(false, 1)
	if err != nil || missing != nil {
		t.Fatalf("Maybe(false) = %v, %v", missing, err)
	}
	present, err := Maybe(true, 2.5)
	if err != nil || present == nil || math.Abs(*present-2.5) > 1e-12 {
		t.Fatalf("Maybe(true) = %v, %v", present, err)
	}
}
`
