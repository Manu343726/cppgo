package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestGeneratedPackageBuildsAndRuns is the end-to-end proof of the emitter: it
// builds the fixture library with g++, generates bindings for it, compiles the
// generated shim into the same library, and then calls the generated Go package
// for real.
//
// The exercise covers both carriers: Scale is called through the direct
// trampoline (noexcept, out of line), everything else through a generated
// thunk. It also proves Open's missing-thunk diagnostic in a second process,
// because a package's library is opened once.
func TestGeneratedPackageBuildsAndRuns(t *testing.T) {
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
	fixture, err := filepath.Abs("testdata/mathx")
	require.NoError(t, err)
	header := filepath.Join(fixture, "vector.hpp")

	tmp := t.TempDir()
	cxxDir := filepath.Join(tmp, "cxx")
	require.NoError(t, copyTree(fixture, cxxDir))
	lib := filepath.Join(tmp, "libmathx.so")
	noShim := filepath.Join(tmp, "libmathx_noshim.so")

	// The library without the shim is what the generator cross-checks the
	// direct binding against; the shim does not exist yet.
	buildLibrary(t, gxx, cxxDir, noShim, "")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "genpkg")
	require.NoError(t, Generate(Config{
		Headers: []string{header},
		Package: "mathx",
		Library: noShim,
		Out:     out,
	}))
	// The shim is compiled into the same library, as the contract requires.
	buildLibrary(t, gxx, cxxDir, lib, filepath.Join(out, ShimSourceFileName))

	// The generated shim must be legal C++ for both compilers, not merely text
	// that g++ accepted when linking.
	for _, compiler := range []string{"g++", "clang++"} {
		path, err := exec.LookPath(compiler)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, "-std=c++17", "-fsyntax-only", "-I"+cxxDir, filepath.Join(out, ShimSourceFileName))
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "%s rejected the generated shim:\n%s", compiler, output)
	}

	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "e2e_test.go"), []byte(e2eTestSource), 0o644))

	runGo(t, goBin, moduleDir, "vet", "./genpkg")
	// The two scenarios run in separate processes: a package's library is
	// opened once, so a process that loaded the shim-bearing library cannot
	// then observe the missing-thunk failure.
	env := []string{"CPPGO_E2E_LIB=" + lib, "CPPGO_E2E_NOSHIM=" + noShim}
	ran := runGoEnv(t, goBin, moduleDir, env, "test", "-count=1", "-v", "-run", "^TestBindings$", "./genpkg")
	assert.Contains(t, ran, "--- PASS: TestBindings")
	ran = runGoEnv(t, goBin, moduleDir, env, "test", "-count=1", "-v", "-run", "^TestMissingShim$", "./genpkg")
	assert.Contains(t, ran, "--- PASS: TestMissingShim")
}

// buildLibrary compiles the fixture and, when shim is non-empty, links the
// generated shim into the same shared object.
func buildLibrary(t *testing.T, gxx, dir, out, shim string) {
	t.Helper()
	args := []string{"-std=c++17", "-O1", "-fPIC", "-shared", "-I" + dir}
	args = append(args, filepath.Join(dir, "vector.cpp"))
	if shim != "" {
		args = append(args, shim)
	}
	args = append(args, "-o", out)
	output, err := exec.Command(gxx, args...).CombinedOutput()
	require.NoErrorf(t, err, "g++ failed:\n%s", output)
}

// writeModule writes the throwaway module that holds the generated package. It
// needs the real runtime, wired up with a replace directive exactly as a user
// would.
func writeModule(t *testing.T, dir, repo string) error {
	t.Helper()
	gomod := "module cppgo-e2e\n\ngo 1.27.0\n\nrequire github.com/Manu343726/cppgo v0.0.0\n\nreplace github.com/Manu343726/cppgo => " + repo + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
}

// runGo runs the go tool in a throwaway module.
func runGo(t *testing.T, goBin, dir string, args ...string) string {
	t.Helper()
	return runGoEnv(t, goBin, dir, nil, args...)
}

// runGoEnv is runGo with extra environment variables.
func runGoEnv(t *testing.T, goBin, dir string, extra []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(goBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOFLAGS=-mod=mod",
		"GOWORK=off",
		"GOPROXY=off",
	)
	cmd.Env = append(cmd.Env, extra...)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "go %s failed:\n%s", strings.Join(args, " "), output)
	return string(output)
}

// copyTree copies a directory tree.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// e2eTestSource is the test the generated package runs. It is written as Go
// source rather than built as a string so the coverage of the generated code is
// the behaviour, not the text.
const e2eTestSource = `package mathx

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestBindings(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_LIB")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector: %v", err)
	}
	if x, err := v.X(); err != nil || x != 3 {
		t.Fatalf("X = %v, %v", x, err)
	}
	if n, err := v.Norm(); err != nil || math.Abs(n-5) > 1e-12 {
		t.Fatalf("Norm = %v, %v", n, err)
	}
	if d, err := v.Dot([]float64{1, 0}); err != nil || math.Abs(d-3) > 1e-12 {
		t.Fatalf("Dot = %v, %v", d, err)
	}

	// The direct carrier: Scale is noexcept and out of line, so this call
	// never enters the shim.
	v.Scale(2)
	if n, err := v.Norm(); err != nil || math.Abs(n-10) > 1e-12 {
		t.Fatalf("Norm after Scale = %v, %v", n, err)
	}

	clone, err := v.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if n, err := clone.Norm(); err != nil || math.Abs(n-10) > 1e-12 {
		t.Fatalf("cloned Norm = %v, %v", n, err)
	}
	if err := clone.Close(); err != nil {
		t.Fatalf("clone Close: %v", err)
	}
	if err := clone.Close(); err == nil {
		t.Fatal("a second Close must report ErrClosed")
	}

	if dims, err := VectorDims(); err != nil || dims != 2 {
		t.Fatalf("VectorDims = %v, %v", dims, err)
	}

	x := []float64{1, 2, 3}
	y := []float64{10, 20, 30}
	if err := Saxpy(2, x, y); err != nil {
		t.Fatalf("Saxpy: %v", err)
	}
	want := []float64{12, 24, 36}
	for i := range want {
		if y[i] != want[i] {
			t.Fatalf("Saxpy y[%d] = %v, want %v", i, y[i], want[i])
		}
	}
	if err := Saxpy(2, x, y[:2]); err == nil {
		t.Fatal("differently sized slices must be an error before the call")
	}

	// The mirrored POD is a Go value with the C layout.
	p := Point{X: 1, Y: 2}
	if p.X+p.Y != 3 {
		t.Fatalf("Point fields are wrong: %+v", p)
	}
	if _, err := NewPoint(); err != nil {
		t.Fatalf("NewPoint: %v", err)
	}
	if c, err := p.Clone(); err != nil || *c != p {
		t.Fatalf("Point.Clone = %+v, %v", c, err)
	}

	// The library opened explicitly; the configured path was never needed.
	if err := Open(""); err != nil {
		t.Fatalf("Open after a successful explicit Open: %v", err)
	}
}

func TestMissingShim(t *testing.T) {
	err := Open(os.Getenv("CPPGO_E2E_NOSHIM"))
	if err == nil {
		t.Fatal("Open must fail when the shim was not compiled into the library")
	}
	if !strings.Contains(err.Error(), "zz_cppgo_shim.cpp") {
		t.Fatalf("the missing-thunk error must name the shim file, got: %v", err)
	}
}
`

// TestFixturePackagesCompile builds every fixture's generated Go package in a
// throwaway module: go vet runs asmdecl, which checks each trampoline's frame
// size and FP offsets against the Go declaration, so a widened or narrowed
// signature cannot pass unnoticed. The shim is compiled separately.
func TestFixturePackagesCompile(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not in PATH")
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	for name, cfg := range goldenFixtures() {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			moduleDir := t.TempDir()
			require.NoError(t, writeModule(t, moduleDir, repo))
			cfg.Out = filepath.Join(moduleDir, "genpkg")
			require.NoError(t, Generate(cfg))
			runGo(t, goBin, moduleDir, "vet", "./genpkg")
			runGo(t, goBin, moduleDir, "build", "./genpkg")
		})
	}
}
