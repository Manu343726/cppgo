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

// TestNestedTypesTakeTheirOuterName pins the Go naming rule for a type declared
// inside a record: a Go package cannot nest a type in a type, so the outer
// record's Go name is a prefix — Outer::Inner is OuterInner, and a type inside a
// specialization carries the specialization's name, Box<int>::Kind is BoxIntKind.
func TestNestedTypesTakeTheirOuterName(t *testing.T) {
	m := renderMap(t, nestedConfig())
	source := m[GenFileName]

	cases := []struct {
		name string
		want []string
	}{
		{
			name: "a nested aggregate",
			want: []string{
				"type OuterInner struct {",
			},
		},
		{
			name: "a nested opaque class",
			want: []string{
				"type OuterHolder struct {",
				"func NewOuterHolder(value int32) (*OuterHolder, error) {",
			},
		},
		{
			name: "a nested enum",
			want: []string{
				"type OuterMode int32",
				"OuterModeOff OuterMode = 0",
				"OuterModeOn  OuterMode = 1",
			},
		},
		{
			name: "an enum two levels deep",
			want: []string{
				"type OuterHolderLevel int32",
				"OuterHolderLevelLow  OuterHolderLevel = 0",
				"OuterHolderLevelHigh OuterHolderLevel = 1",
			},
		},
		{
			name: "a type synthesized for a specialization",
			want: []string{
				"type BoxInt struct {",
				"type BoxIntSlot struct {",
				"type BoxIntKind int32",
				"BoxIntKindEmpty BoxIntKind = 0",
				"type BoxFloat64Kind int32",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.want {
				assert.Contains(t, source, want)
			}
		})
	}

	// A member that names the nested type resolves against the specialization,
	// and the generic API says what it omitted instead of guessing.
	assert.Contains(t, source, "func (o *Outer) Make(x int32, y int32) OuterInner {")
	assert.Contains(t, source, "func (o *Outer) Mode() OuterMode {")
	// libclang exposes no definition cursor for a specialization synthesized
	// from a pattern, so it answers nothing about the layout of the type or of
	// its nested types. The model completes both from the substituted fields
	// (issue #102, 4c), so the factory exists and the plain nested record is a
	// Go struct rather than an opaque value with accessors.
	assert.Contains(t, source, "func NewBoxInt() (*BoxInt, error) {")
	assert.Contains(t, source, "type BoxIntSlot struct {")
	assert.Contains(t, source, "func NewBoxIntSlot() (*BoxIntSlot, error) { return &BoxIntSlot{}, nil }")
	assert.NotContains(t, source, "no layout the Go side can allocate")
}

// TestGenericAPIOmitsAMemberNamingANestedType is the report half: the generic
// Box<T> cannot type a member whose result is per-instantiation, so the emitter
// skips that member, warns, and leaves it to the concrete specialization.
func TestGenericAPIOmitsAMemberNamingANestedType(t *testing.T) {
	m := renderMap(t, nestedConfig())
	assert.Contains(t, m[GenFileName], "type Box[T any] struct {")
	assert.Contains(t, m[GenFileName], "func (b *Box[T]) Get() (T, error) {")

	var result Result
	cfg := nestedConfig()
	cfg.Warnings = nil
	cfg.Report = nil
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	_ = result
	joined := ""
	for _, warning := range rendered.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "nested::Box::slot() const noexcept is not part of the generic API")
	assert.Contains(t, joined, "nested::Box::kind() const noexcept is not part of the generic API")
}

// TestNestedTypeRefusalIsGone is the negative half.
func TestNestedTypeRefusalIsGone(t *testing.T) {
	outs, err := Render(nestedConfig())
	require.NoError(t, err)
	for _, out := range outs {
		assert.NotContains(t, string(out.Data), "a type nested inside a record is not emitted")
	}
}

// TestNestedBindingsRunAgainstTheLibrary proves the naming holds at run time:
// the values come back typed, through the nested enum and the two-level type.
func TestNestedBindingsRunAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/nested")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libnested.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "nested.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "nested")
	var report bytes.Buffer
	cfg := nestedConfig()
	cfg.Library = library
	cfg.Out = out
	cfg.Report = &report
	require.NoError(t, Generate(cfg))

	shim := filepath.Join(out, ShimLibraryName("nested"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "nested_test.go"), []byte(nestedE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./nested")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./nested"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./nested"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestNested")
		})
	}
}

// nestedConfig is the nested fixture's configuration, instantiated for both
// concrete types the header declares.
func nestedConfig() Config {
	cfg := fixtureConfig("nested", "nested.hpp")
	cfg.Instantiate = []string{"nested::Box<int>", "nested::Box<double>"}
	return cfg
}

// nestedE2ESource is the program the generated nested package runs.
const nestedE2ESource = `package nested

import (
	"os"
	"testing"
)

func TestNested(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// A nested aggregate, built and read as a Go struct.
	outer, err := NewOuter()
	if err != nil {
		t.Fatalf("NewOuter: %v", err)
	}
	inner := outer.Make(3, 4)
	if inner.X != 3 || inner.Y != 4 {
		t.Fatalf("Make = %v", inner)
	}

	// The nested enum, with its own Go type.
	if got := outer.Mode(); got != OuterModeOn {
		t.Fatalf("Mode = %v, want On", got)
	}

	// A type two levels inside a record.
	holder, err := NewOuterHolder(5)
	if err != nil {
		t.Fatalf("NewOuterHolder: %v", err)
	}
	if got := holder.Value(); got != 5 {
		t.Fatalf("Holder.Value = %v", got)
	}
	if got := holder.Level(); got != OuterHolderLevelHigh {
		t.Fatalf("Holder.Level = %v, want High", got)
	}

	// The synthesized specialization is a Go type of its own, with an enum of
	// its own. Its storage cannot be allocated by Go — libclang completes no
	// layout for a specialization synthesized from a pattern — so the fixture
	// measures the types and the constants, and the finding is reported.
	if BoxIntKindFull != 1 || BoxFloat64KindEmpty != 0 {
		t.Fatalf("the synthesized enums are wrong: %v %v", BoxIntKindFull, BoxFloat64KindEmpty)
	}
}
`
