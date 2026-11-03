package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
	"github.com/Manu343726/cppgo/internal/gen"
)

// abiHeader is the real mathx header, which the abi-mode CLI tests bind. The
// tests skip when libclang is missing, exactly as the generator tests do.
func abiHeader(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "internal", "cxx", "testdata", "mathx", "mathx.hpp"))
	require.NoError(t, err)
	return path
}

// writeABIPackage materialises a small direct-ABI package.
func writeABIPackage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `// Package mathx binds mathx::Vector directly.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ` + abiHeader(t) + `

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)

//cppgo:method norm
func (v Vector) Norm() float64

//cppgo:dtor
func (v Vector) Destroy()
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mathx.go"), []byte(src), 0o644))
	return dir
}

// TestGenABIReport checks the report printed by "cppgo gen": one line per
// binding, naming the C++ declaration, the mangled symbol and the tier.
func TestGenABIReport(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := writeABIPackage(t)
	out := t.TempDir()

	code, stdout, stderr := exec("gen", "-dir", dir, "-out", out)
	require.Equal(t, exitOK, code, stderr)
	assert.Contains(t, stdout, "mathx::Vector::Vector")
	assert.Contains(t, stdout, "mangled _ZN5mathx6VectorC1Edd")
	assert.Contains(t, stdout, "mathx::Vector::norm")
	assert.Contains(t, stdout, "mangled _ZNK5mathx6Vector4normEv")
	assert.Contains(t, stdout, "mathx::Vector::~Vector")
	assert.Contains(t, stdout, "abi from")
	// The flags are the generator's fallback here, and it says so.
	assert.Contains(t, stderr, "warning:")
	assert.Contains(t, stderr, "assumption")

	// The direct ABI mode writes no shim header.
	assert.NoFileExists(t, filepath.Join(out, gen.ShimFileName))
	assert.FileExists(t, filepath.Join(out, gen.GenFileName))
	assert.FileExists(t, filepath.Join(out, gen.AsmFileName))
	assert.FileExists(t, filepath.Join(out, gen.SourceFileName))
}

// TestGenABIQuiet checks that -q silences the report but not the warnings.
func TestGenABIQuiet(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := writeABIPackage(t)
	code, stdout, stderr := exec("gen", "-dir", dir, "-out", t.TempDir(), "-q")
	require.Equal(t, exitOK, code, stderr)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "warning:")
}

// TestCheckABIIsSilent checks that "cppgo check" does not print the report: it
// is a gate, and its output is the verdict.
func TestCheckABIIsSilent(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := writeABIPackage(t)
	out := t.TempDir()
	require.Equal(t, exitOK, mustExec(t, "gen", "-dir", dir, "-out", out, "-q"))

	code, stdout, stderr := exec("check", "-dir", dir, "-out", out)
	require.Equal(t, exitOK, code, stderr)
	assert.Contains(t, stdout, "up to date")
	assert.NotContains(t, stdout, "mangled")
}

// TestGenABIValidationError checks the exit code and the message of a binding
// that cannot work: file:line, the C++ name, and what to do about it.
func TestGenABIValidationError(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := t.TempDir()
	src := `package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ` + abiHeader(t) + `

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:method x
func (v Vector) X() float64
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mathx.go"), []byte(src), 0o644))

	code, stdout, stderr := exec("gen", "-dir", dir, "-out", t.TempDir())
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "cppgo gen:")
	assert.Contains(t, stderr, "mathx.go:")
	assert.Contains(t, stderr, "mathx::Vector::x")
	assert.Contains(t, stderr, "move the definition out into a source file")
}

// TestGenABIForce checks the escape hatch from the command line.
func TestGenABIForce(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := t.TempDir()
	src := `package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ` + abiHeader(t) + `

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:method x
func (v Vector) X() float64
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mathx.go"), []byte(src), 0o644))
	out := t.TempDir()

	code, stdout, _ := exec("gen", "-dir", dir, "-out", out, "-force")
	require.Equal(t, exitOK, code)
	assert.Contains(t, stdout, "forced with -force, NOT validated")
	data, err := os.ReadFile(filepath.Join(out, gen.GenFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), "NOT VALIDATED")
}

// TestGenABICompileCommandsFlag checks that -compile-commands is accepted and
// reaches the resolver: a path that does not hold a database is an error.
func TestGenABICompileCommandsFlag(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := writeABIPackage(t)
	code, stdout, stderr := exec("gen", "-dir", dir, "-out", t.TempDir(), "-compile-commands", filepath.Join(t.TempDir(), "nope"))
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "compile_commands.json")
}

// TestGenABIStaleShimHeader checks that a leftover shim header is reported.
func TestGenABIStaleShimHeader(t *testing.T) {
	if !libclangOrSkip(t) {
		return
	}
	dir := writeABIPackage(t)
	out := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(out, gen.ShimFileName), []byte("// stale\n"), 0o644))

	code, _, stderr := exec("gen", "-dir", dir, "-out", out, "-q")
	require.Equal(t, exitOK, code, stderr)
	assert.Contains(t, stderr, "left by an earlier shim-mode run")
}

// TestGenHelpDocumentsABIFlags keeps the help text honest.
func TestGenHelpDocumentsABIFlags(t *testing.T) {
	code, stdout, stderr := exec("gen", "-h")
	require.Equal(t, exitOK, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "//cppgo:header")
	assert.Contains(t, stdout, "//cppgo:class")
	assert.Contains(t, stdout, "-force")
	assert.Contains(t, stdout, "-compile-commands")
}

// libclangOrSkip skips the test when the direct ABI mode cannot run here.
func libclangOrSkip(t *testing.T) bool {
	t.Helper()
	library, err := cxx.Load()
	if err != nil {
		t.Skipf("libclang is not available: %v", err)
		return false
	}
	_ = library.Close()
	return true
}
