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

// TestABIEndToEnd is the proof that the direct ABI mode is real: it builds
// mathx.cpp into a shared object with g++, generates bindings for the real
// internal/cxx/testdata/mathx class, compiles the generated package (go vet
// runs asmdecl, which checks every FP offset and frame size against the
// declarations) and then calls a constructor, a const method and a method
// taking a slice, asserting the numbers.
//
// A golden file would only prove the text. This proves the ABI: `this` in RDI,
// the float arguments in XMM0/XMM1, the slice as (pointer, length) and the
// result in XMM0.
func TestABIEndToEnd(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	if _, err := cxx.Load(); err != nil {
		t.Skipf("libclang is not available: %v", err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not in PATH")
	}

	header, err := filepath.Abs("../cxx/testdata/mathx/mathx.hpp")
	require.NoError(t, err)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	tmp := t.TempDir()
	lib := filepath.Join(tmp, "libmathx.so")
	build := exec.Command(gxx, "-std=c++17", "-O1", "-fPIC", "-shared",
		filepath.Join(filepath.Dir(header), "mathx.cpp"), "-o", lib)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("g++ failed: %v\n%s", err, out)
	}

	// The DSL package. Only what the direct ABI mode can express: an
	// out-of-line constructor, an out-of-line const method, a method taking a
	// (pointer, length) pair, a destructor and a free function.
	src := filepath.Join(tmp, "src")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "mathx.go"), []byte(`// Package mathx binds mathx::Vector through the direct C++ ABI mode.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header `+header+`

// Vector mirrors mathx::Vector. A defined type over cppgo.Handle, so the
// receiver is the object pointer.
//cppgo:class mathx::Vector
type Vector cppgo.Handle

// New constructs a Vector.
//cppgo:ctor
func New(x, y float64) (Vector, error)

// Norm is the Euclidean norm of the vector.
//cppgo:method norm
func (v Vector) Norm() float64

// Dot is the dot product with another vector's components.
//cppgo:method dot
func (v Vector) Dot(ys []float64) float64

// Destroy runs the destructor and releases the object.
//cppgo:dtor
func (v Vector) Destroy()

// Add adds two ints.
//cppgo:function mathx::add
func Add(a, b int32) int32
`), 0o644))

	moduleDir := filepath.Join(tmp, "module")
	require.NoError(t, copyTree(filepath.Join("testdata", "abi", "e2e"), moduleDir))
	require.NoError(t, writeModule(t, moduleDir, repo))

	out := filepath.Join(moduleDir, "genpkg")
	require.NoError(t, Generate(Config{Dir: src, Out: out}))

	// The generated package must be legal Go with legal assembly, and then it
	// must run.
	runGoModule(t, goBin, moduleDir, "vet", "./genpkg")
	ran := runGoModuleEnv(t, goBin, moduleDir, []string{"CPPGO_TEST_LIB=" + lib}, "test", "-count=1", "-v", "./genpkg")
	assert.Contains(t, ran, "ok", "the generated package must pass its own test")
	assert.Contains(t, ran, "--- PASS: TestVectorABI", ran)
}

// writeModule writes the go.mod of the throwaway module that holds the
// generated package: it needs the real cppgo runtime, which is local, so it is
// wired up with a replace directive exactly as a user would.
func writeModule(t *testing.T, dir, repo string) error {
	t.Helper()
	gomod := "module cppgo-abi-e2e\n\ngo 1.25.0\n\nrequire github.com/Manu343726/cppgo v0.0.0\n\nreplace github.com/Manu343726/cppgo => " + repo + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	// The repository's own go.sum already pins every dependency: copying it
	// keeps the run offline.
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
}

// runGoModule runs the go tool in a throwaway module.
func runGoModule(t *testing.T, goBin, dir string, args ...string) string {
	t.Helper()
	return runGoModuleEnv(t, goBin, dir, nil, args...)
}

// runGoModuleEnv is runGoModule with extra environment variables.
func runGoModuleEnv(t *testing.T, goBin, dir string, extra []string, args ...string) string {
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
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go %s failed:\n%s", strings.Join(args, " "), out)
	return string(out)
}
