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

// TestOperatorBindingsRunAgainstTheLibrary is the operator construct's
// end-to-end proof: a real shared object, the generated package, and every name
// the contract's table gives called with values the C++ produced. A method that
// returns a reference hands back a view of the same object, so the reference
// count is measured too.
func TestOperatorBindingsRunAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/operators")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "liboperators.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "operators.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "operators")
	var report bytes.Buffer
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join(fixture, "operators.hpp")},
		Package: "operators",
		Library: library,
		Out:     out,
		Report:  &report,
	}))

	// The free operator that returns a POD by value is a thunk; its carrier is
	// not the point, the name is.
	carrierOf(t, report.String(), "ops::operator+(const Vec2 &, const Vec2 &)", "thunk")

	shim := filepath.Join(out, ShimLibraryName("operators"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "operators_test.go"), []byte(operatorE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./operators")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./operators"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./operators"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestOperators")
		})
	}
}

// operatorE2ESource is the program the generated operator package runs.
const operatorE2ESource = `package operators

import (
	"math"
	"os"
	"testing"
)

func TestOperators(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The free operators that return the mirrored POD by value.
	a := Vec2{X: 1, Y: 2}
	b := Vec2{X: 3, Y: 4}
	sum, err := Add(&a, &b)
	if err != nil || sum.X != 4 || sum.Y != 6 {
		t.Fatalf("Add = %v, %v", sum, err)
	}
	diff, err := Sub(&a, &b)
	if err != nil || diff.X != -2 || diff.Y != -2 {
		t.Fatalf("Sub = %v, %v", diff, err)
	}
	if eq, err := Equal(&a, &a); err != nil || !eq {
		t.Fatalf("Equal(a, a) = %v, %v", eq, err)
	}
	if ne, err := NotEqual(&a, &b); err != nil || !ne {
		t.Fatalf("NotEqual(a, b) = %v, %v", ne, err)
	}

	// The free operator with a scalar operand.
	n, err := NewNumber(5)
	if err != nil {
		t.Fatalf("NewNumber: %v", err)
	}
	if scaled, err := Mul(n, 2.5); err != nil || scaled != 12.5 {
		t.Fatalf("Mul(n, 2.5) = %v, %v", scaled, err)
	}

	// The member operators.
	other, err := NewNumber(3)
	if err != nil {
		t.Fatalf("NewNumber: %v", err)
	}
	view, err := n.AddAssign(other)
	if err != nil {
		t.Fatalf("AddAssign: %v", err)
	}
	if v := n.Value(); v != 8 {
		t.Fatalf("after AddAssign, Value = %v, want 8", v)
	}
	// The returned reference is a view of the same object.
	if v := view.Value(); v != 8 {
		t.Fatalf("the view sees %v, want 8", v)
	}
	if err := view.Close(); err != nil {
		t.Fatalf("closing the view: %v", err)
	}
	if v := n.Value(); v != 8 {
		t.Fatalf("the object survives its view: Value = %v", v)
	}

	if eq, err := n.Equal(other); err != nil || eq {
		t.Fatalf("Equal(8, 3) = %v, %v", eq, err)
	}
	if less, err := other.Less(n); err != nil || !less {
		t.Fatalf("Less(3, 8) = %v, %v", less, err)
	}
	if at, err := n.At(1); err != nil || at != 9 {
		t.Fatalf("At(1) = %v, %v", at, err)
	}
	if call, err := n.Call(2, 1); err != nil || call != 17 {
		t.Fatalf("Call(2, 1) = %v, %v", call, err)
	}

	// The conversion operators.
	if f, err := n.Float64(); err != nil || f != 8 {
		t.Fatalf("Float64 = %v, %v", f, err)
	}
	if i, err := n.Int32(); err != nil || i != 8 {
		t.Fatalf("Int32 = %v, %v", i, err)
	}
	if b, err := n.Bool(); err != nil || !b {
		t.Fatalf("Bool = %v, %v", b, err)
	}
	if str, err := n.String(); err != nil || str != "8.000000" {
		t.Fatalf("String = %q, %v", str, err)
	}
	if zero, err := NewNumber(0); err == nil {
		if b, err := zero.Bool(); err != nil || b {
			t.Fatalf("Bool of zero = %v, %v", b, err)
		}
	}
	_ = math.Abs(0)
}
`
