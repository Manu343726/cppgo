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

// TestStatusConventionShape pins the -status contract end to end at the source
// level: the Go signature returns only the error, the integer disappears from
// it, and the thunk returns the callee's own code so the Go side has something
// to test.
func TestStatusConventionShape(t *testing.T) {
	files := renderMap(t, statusConfig())

	gen := files[GenFileName]
	assert.Contains(t, gen, "func Parse(text string) error", "the status integer is not in the Go signature")
	assert.NotContains(t, gen, "func Parse(text string) (int32, error)")
	assert.Contains(t, gen, `cppgoStatus := cppgoCall_`, "the wrapper tests the code it got")

	shim := files[ShimSourceFileName]
	assert.Contains(t, shim, "return ::status::parse(cppgo_text);",
		"the thunk returns the callee's status, not a literal 0")
}

// TestStatusRefusals covers the two entries -status cannot use: one that names
// nothing, and one whose return is a value rather than a status.
func TestStatusRefusals(t *testing.T) {
	cases := []struct {
		name   string
		status []string
		want   string
	}{
		{"unmatched", []string{"status::nope"}, "it names nothing this header declares"},
		{"a value result", []string{"status::ratio"}, "and a status is a 32-bit integer"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := statusConfig()
			cfg.Status = tc.status
			_, err := Render(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestStatusEndToEnd runs the convention against a real library: success is a
// nil error, a non-zero code is a *cppgo.CallError naming the declaration.
func TestStatusEndToEnd(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	if _, err := cxx.Load(); err != nil {
		t.Skipf("libclang is not available: %v", err)
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	tmp := t.TempDir()
	header, err := filepath.Abs(filepath.Join("testdata", "status", "status.hpp"))
	require.NoError(t, err)
	source, err := filepath.Abs(filepath.Join("testdata", "status", "status.cpp"))
	require.NoError(t, err)

	// The user's library, built by its own command.
	library := filepath.Join(tmp, "libstatus.so")
	output, err := exec.Command(gxx, "-std=c++17", "-O1", "-fPIC", "-shared", "-I",
		filepath.Dir(header), source, "-o", library).CombinedOutput()
	require.NoErrorf(t, err, "g++ failed:\n%s", output)

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "statusx")
	require.NoError(t, Generate(Config{
		Headers: []string{header},
		Package: "statusx",
		Library: library,
		Out:     out,
		Status:  []string{"status::parse"},
	}))
	shim := filepath.Join(out, ShimLibraryName("statusx"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)

	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "status_test.go"), []byte(statusTestSource), 0o644))
	ran := runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=0", "CPPGO_STATUS_SHIM=" + shim},
		"test", "-count=1", "-v", "./statusx")
	assert.Contains(t, ran, "--- PASS: TestStatus")
}

// statusConfig is the fixture run with the convention applied to the one
// declaration that has it.
func statusConfig() Config {
	return Config{
		Headers: []string{filepath.Join("testdata", "status", "status.hpp")},
		Package: "statusx",
		Library: "libstatus",
		Out:     ".",
		Status:  []string{"status::parse"},
	}
}

// statusTestSource is the program the generated package runs.
const statusTestSource = `package statusx

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Manu343726/cppgo"
)

func TestStatus(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_STATUS_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := Parse("ok"); err != nil {
		t.Fatalf("Parse(ok) = %v, want nil: status 0 is success", err)
	}
	err := Parse("")
	if err == nil {
		t.Fatal("Parse(empty) must report the non-zero status")
	}
	var call *cppgo.CallError
	if !errors.As(err, &call) || call.Symbol != "status::parse" {
		t.Fatalf("Parse(empty) = %v, want a CallError naming status::parse", err)
	}
	if !strings.Contains(err.Error(), "status::parse") {
		t.Fatalf("the error must name the declaration: %v", err)
	}
}
`
