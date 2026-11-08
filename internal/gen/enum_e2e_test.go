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

// TestEnumBindingsRunAgainstTheLibrary is the enum construct's end-to-end
// proof: the fixture is compiled into a real shared object, cppgo binds the
// header, and the generated package is built and run twice against the two
// objects. A scoped enum takes the direct carrier, an unscoped one and a
// byte-sized one cross as their underlying integer, and an enum returned by a
// thunk comes back as the named Go type.
func TestEnumBindingsRunAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/enums")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libenums.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "enums.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "enums")
	var report bytes.Buffer
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join(fixture, "enums.hpp")},
		Package: "enums",
		Library: library,
		Out:     out,
		Report:  &report,
	}))

	// A noexcept, out-of-line function that only takes and returns an enum is
	// register representable, so it is called by the trampoline.
	carrierOf(t, report.String(), "enums::opposite(Axis) noexcept", "direct")
	carrierOf(t, report.String(), "enums::next_mode(Mode) noexcept", "direct")

	shim := filepath.Join(out, ShimLibraryName("enums"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)

	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "enums_test.go"), []byte(enumE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./enums")
	passes := []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./enums"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./enums"},
		},
	}
	for _, pass := range passes {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestEnums")
		})
	}
}

// enumE2ESource is the program the generated enum package runs. Every
// assertion is a value the C++ produced.
const enumE2ESource = `package enums

import (
	"os"
	"testing"
)

func TestEnums(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The constants carry the values the header declared.
	if AxisX != 0 || AxisY != 1 || AxisZ != 2 {
		t.Fatalf("the axis constants are %v %v %v", AxisX, AxisY, AxisZ)
	}
	if OffsetBehind != -3 || OffsetFar != 900 {
		t.Fatalf("the offset constants are %v %v", OffsetBehind, OffsetFar)
	}
	if ModeOn != 200 || ModeMaximum != 255 {
		t.Fatalf("the mode constants are %v %v", ModeOn, ModeMaximum)
	}
	if FlagsExecute != 7 {
		t.Fatalf("FlagsExecute = %v", FlagsExecute)
	}

	// An enum in and out of a direct call.
	if got := Opposite(AxisX); got != AxisY {
		t.Fatalf("Opposite(X) = %v, want Y", got)
	}
	if got := Opposite(AxisZ); got != AxisX {
		t.Fatalf("Opposite(Z) = %v, want X (the default case)", got)
	}
	if got := NextMode(ModeOff); got != ModeOn {
		t.Fatalf("NextMode(Off) = %v, want On", got)
	}
	if got := NextMode(ModeOn); got != ModeOff {
		t.Fatalf("NextMode(On) = %v, want Off", got)
	}

	// Two enum parameters in the two INTEGER slots.
	if !HasFlag(FlagsRead|FlagsWrite, FlagsWrite) {
		t.Fatal("HasFlag(Read|Write, Write) = false")
	}
	if HasFlag(FlagsRead, FlagsWrite) {
		t.Fatal("HasFlag(Read, Write) = true")
	}
	if got := AxisCount(); got != 3 {
		t.Fatalf("AxisCount = %v", got)
	}

	// The same three signatures through the shim, where the enum is cast back
	// to its C++ type on the way in and to its underlying integer on the way out.
	if got := InlineOpposite(AxisX); got != AxisY {
		t.Fatalf("InlineOpposite(X) = %v, want Y", got)
	}
	if !InlineHasFlag(FlagsRead|FlagsWrite, FlagsRead) {
		t.Fatal("InlineHasFlag(Read|Write, Read) = false")
	}
	if got := InlineNextMode(ModeOn); got != ModeOff {
		t.Fatalf("InlineNextMode(On) = %v, want Off", got)
	}
}
`
