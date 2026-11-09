package gen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultArgumentsProduceOneFunctionPerPrefix pins the contract's rule: a
// declaration with trailing defaults becomes one Go function per allowed prefix,
// the full signature keeps the bare name, a shorter prefix takes the parameter
// type suffix, and every prefix is a thunk because the default is applied by the
// caller's compiler.
func TestDefaultArgumentsProduceOneFunctionPerPrefix(t *testing.T) {
	m := renderMap(t, fixtureConfig("defaults", "defaults.hpp"))
	source := m[GenFileName]
	shim := m[ShimSourceFileName]

	// A constructor with two defaults: the full signature, one prefix, and the
	// zero-argument prefix dropped because the bare name is the full one's.
	assert.Contains(t, source, "func NewShape(sides int32, scale float64) (*Shape, error) {")
	assert.Contains(t, source, "func NewShapeInt32(sides int32) (*Shape, error) {")
	assert.NotContains(t, source, "func NewShape() (*Shape, error)")

	// A member function with two defaults.
	assert.Contains(t, source, "func (s *Shape) Scale(factor float64, clamp bool) error {")
	assert.Contains(t, source, "func (s *Shape) ScaleFloat64(factor float64) error {")

	// A free function whose result is a standard-library value.
	assert.Contains(t, source, "func Greet(name string, greeting string) (string, error) {")
	assert.Contains(t, source, "func GreetString(name string) (string, error) {")

	// The shim writes the shorter call, so C++ inserts the default.
	assert.Contains(t, shim, "new (self) ::defaults::Shape(sides);")
	assert.Contains(t, shim, "::defaults::greet(")
	assert.Contains(t, shim, "static_cast<::defaults::Shape *>(self)->scale(")

	// The report names the reason a prefix is a thunk.
	rendered, err := RenderResult(fixtureConfig("defaults", "defaults.hpp"))
	require.NoError(t, err)
	joined := ""
	for _, warning := range rendered.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "the zero-argument spelling is dropped")
	report := ""
	for _, line := range rendered.Report {
		report += line + "\n"
	}
	assert.Contains(t, report, "default arguments: the caller's compiler applies them")
}

// TestDefaultArgumentPrefixesRunAgainstTheLibrary proves the defaults actually
// apply: the shorter call reaches the same C++ function with the declared
// default, which is the only measurement that distinguishes it from passing
// zero.
func TestDefaultArgumentPrefixesRunAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/defaults")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libdefaults.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "defaults.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "defaults")
	var report bytes.Buffer
	cfg := fixtureConfig("defaults", "defaults.hpp")
	cfg.Headers = []string{filepath.Join(fixture, "defaults.hpp")}
	cfg.Library = library
	cfg.Out = out
	cfg.Report = &report
	require.NoError(t, Generate(cfg))

	shim := filepath.Join(out, ShimLibraryName("defaults"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "defaults_test.go"), []byte(defaultsE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./defaults")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./defaults"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./defaults"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestDefaults")
		})
	}
}

// defaultsE2ESource is the program the generated defaults package runs.
const defaultsE2ESource = `package defaults

import (
	"os"
	"testing"
)

func TestDefaults(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The full signature: both arguments are the caller's.
	full, err := NewShape(3, 2)
	if err != nil {
		t.Fatalf("NewShape: %v", err)
	}
	if got := full.Sides(); got != 3 {
		t.Fatalf("Sides = %v, want 3", got)
	}
	if got := full.ScaleValue(); got != 2 {
		t.Fatalf("ScaleValue = %v, want 2", got)
	}

	// The prefix: the second argument is the declared default, 1.0.
	prefix, err := NewShapeInt32(5)
	if err != nil {
		t.Fatalf("NewShapeInt32: %v", err)
	}
	if got := prefix.Sides(); got != 5 {
		t.Fatalf("Sides = %v, want 5", got)
	}
	if got := prefix.ScaleValue(); got != 1 {
		t.Fatalf("ScaleValue = %v, want the default 1", got)
	}

	// The member function: the default factor is 2.0 and the default clamp is
	// true, so a scale of 60 clamps to 100.
	big, err := NewShape(4, 60)
	if err != nil {
		t.Fatalf("NewShape: %v", err)
	}
	if err := big.ScaleFloat64(2); err != nil {
		t.Fatalf("ScaleFloat64: %v", err)
	}
	if got := big.ScaleValue(); got != 100 {
		t.Fatalf("ScaleValue after the clamped scaling = %v, want 100", got)
	}

	// The full signature with clamp off does not clamp.
	free, err := NewShape(4, 60)
	if err != nil {
		t.Fatalf("NewShape: %v", err)
	}
	if err := free.Scale(2, false); err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if got := free.ScaleValue(); got != 120 {
		t.Fatalf("ScaleValue with clamp off = %v, want 120", got)
	}

	// The free function: the prefix passes one string and C++ adds "hello".
	if got, err := GreetString("world"); err != nil || got != "hello world" {
		t.Fatalf("GreetString = %q, %v", got, err)
	}
	if got, err := Greet("world", "hi"); err != nil || got != "hi world" {
		t.Fatalf("Greet = %q, %v", got, err)
	}
}
`
