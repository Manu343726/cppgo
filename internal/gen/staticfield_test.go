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

// TestStaticDataMembersArePackageFunctions pins the contract's shape for a
// static data member: the getter is <Type><Field>(), the setter exists only for
// a mutable member and is <Type>Set<Field>(v), and both are carried by a thunk
// because the member's exported symbol is data, not a function.
func TestStaticDataMembersArePackageFunctions(t *testing.T) {
	m := renderMap(t, fixtureConfig("staticdata", "staticdata.hpp"))
	source := m[GenFileName]
	shim := m[ShimSourceFileName]

	assert.Contains(t, source, "func CounterCount() int32 {")
	// A scalar assignment cannot throw, so the setter has no error result; a
	// standard-library assignment allocates, so it has one.
	assert.Contains(t, source, "func CounterSetCount(count int32) {")
	assert.Contains(t, source, "func CounterLimit() int32 {")
	assert.Contains(t, source, "func CounterLabel() (string, error) {")
	assert.Contains(t, source, "func CounterSetLabel(label string) error {")
	assert.Contains(t, source, "func CounterName() (string, error) {")

	// A const member has no setter.
	assert.NotContains(t, source, "CounterSetLimit")
	assert.NotContains(t, source, "CounterSetName")

	// The thunk reads and writes the storage as C++.
	assert.Contains(t, shim, "return ::statics::Counter::count;")
	assert.Contains(t, shim, "::statics::Counter::count = ")
	assert.Contains(t, shim, "auto cppgo_value = ::statics::Counter::label;")

	// The report names the member, not a call.
	assert.Contains(t, m[GenFileName], "CounterCount reads statics::Counter::count.")
	assert.Contains(t, m[GenFileName], "CounterSetCount sets statics::Counter::count.")
}

// TestStaticDataMemberSetterIsRefusedForConstants proves the setter rule is
// decided from the declaration's constness, not from a manifest entry.
func TestStaticDataMemberSetterIsRefusedForConstants(t *testing.T) {
	header := writeHeader(t, "consts.hpp", `
namespace consts {
struct Limits {
    static const int max = 7;
};
}
`)
	m := renderMap(t, Config{Headers: []string{header}, Package: "consts", Library: "libconsts", Out: "."})
	assert.Contains(t, m[GenFileName], "func LimitsMax() int32 {")
	assert.NotContains(t, m[GenFileName], "LimitsSetMax")
}

// TestStaticDataMembersRunAgainstTheLibrary proves the getter and the setter on
// a real shared object: the value the setter wrote is what the getter reads,
// across the C++ translation unit.
func TestStaticDataMembersRunAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/staticdata")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libstaticdata.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "staticdata.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "staticdata")
	var report bytes.Buffer
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join(fixture, "staticdata.hpp")},
		Package: "staticdata",
		Library: library,
		Out:     out,
		Report:  &report,
	}))
	carrierOf(t, report.String(), "statics::Counter::count", "thunk")

	shim := filepath.Join(out, ShimLibraryName("staticdata"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "staticdata_test.go"), []byte(staticDataE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./staticdata")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./staticdata"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./staticdata"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestStaticData")
		})
	}
}

// staticDataE2ESource is the program the generated static-data package runs.
const staticDataE2ESource = `package staticdata

import (
	"os"
	"testing"
)

func TestStaticData(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The const members: the in-class initialiser and the out-of-line one.
	if got := CounterLimit(); got != 100 {
		t.Fatalf("CounterLimit = %v, want 100", got)
	}
	if got, err := CounterName(); err != nil || got != "counter" {
		t.Fatalf("CounterName = %q, %v", got, err)
	}

	// The mutable scalar: the setter writes the C++ storage and the getter
	// reads it back through a different call.
	if got := CounterCount(); got != 0 {
		t.Fatalf("CounterCount = %v, want the C++ initialiser 0", got)
	}
	CounterSetCount(42)
	if got := CounterCount(); got != 42 {
		t.Fatalf("CounterCount after the write = %v, want 42", got)
	}

	// The mutable string member.
	if got, err := CounterLabel(); err != nil || got != "shared" {
		t.Fatalf("CounterLabel = %q, %v", got, err)
	}
	if err := CounterSetLabel("changed"); err != nil {
		t.Fatalf("CounterSetLabel: %v", err)
	}
	if got, err := CounterLabel(); err != nil || got != "changed" {
		t.Fatalf("CounterLabel after the write = %q, %v", got, err)
	}

	// The static function beside the data is a direct call.
	if got := CounterInstances(); got != 1 {
		t.Fatalf("CounterInstances = %v", got)
	}
}
`
