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

// TestOpaqueRecordFieldsAreAccessors pins the accessor rule: a public field of
// an opaque record is reachable through Field() and, unless it is const,
// SetField(v). The Go type of each accessor follows the type map: a scalar is a
// value, a pointer field is a pointer, a mirrored record is a value, and a
// record the Go side cannot hold by value is a view of the field.
func TestOpaqueRecordFieldsAreAccessors(t *testing.T) {
	m := renderMap(t, fixtureConfig("fields", "fields.hpp"))
	source := m[GenFileName]
	shim := m[ShimSourceFileName]

	assert.Contains(t, source, "func (s *Settings) Count() int32 {")
	assert.Contains(t, source, "func (s *Settings) SetCount(count int32) {")
	assert.Contains(t, source, "func (s *Settings) Kind() Kind {")
	assert.Contains(t, source, "func (s *Settings) SetKind(kind Kind) {")
	assert.Contains(t, source, "func (s *Settings) Source() *int32 {")
	assert.Contains(t, source, "func (s *Settings) SetSource(source *int32) {")
	assert.Contains(t, source, "func (s *Settings) Origin() Point {")
	assert.Contains(t, source, "func (s *Settings) SetOrigin(origin Point) {")
	assert.Contains(t, source, "func (s *Settings) Payload() (*Payload, error) {")
	assert.Contains(t, source, "func (s *Settings) SetPayload(payload *Payload) error {")
	assert.Contains(t, source, "func (s *Settings) Version() int32 {")

	// A const field has no setter.
	assert.NotContains(t, source, "SetVersion")

	// Every accessor is a thunk: a member's address is not a symbol to call.
	assert.Contains(t, shim, "static_cast<const ::fields::Settings *>(self)->count")
	assert.Contains(t, shim, "static_cast<::fields::Settings *>(self)->count = ")
	assert.Contains(t, shim, "static_cast<::fields::Settings *>(self)->origin = *static_cast<const ::fields::Point *>(origin)")

	// A record field the Go side cannot hold by value is reached as a view of
	// the field inside the receiver's object.
	assert.Contains(t, shim, "const_cast<::fields::Payload *>(&(static_cast<const ::fields::Settings *>(self)->payload))")
	assert.Contains(t, source, "return &Payload{obj: s.obj, addr: uintptr(cppgoResult)}, nil")

	// The doc says what the accessor does.
	assert.Contains(t, source, "Count reads fields::Settings::count.")
	assert.Contains(t, source, "SetCount sets fields::Settings::count.")
}

// TestOpaqueFieldRefusalIsGone is the negative half: a public field of an opaque
// record no longer stops the generation with "make the field private".
func TestOpaqueFieldRefusalIsGone(t *testing.T) {
	outs, err := Render(fixtureConfig("fields", "fields.hpp"))
	require.NoError(t, err)
	require.NotEmpty(t, outs)
	for _, out := range outs {
		assert.NotContains(t, string(out.Data), "is not emitted by this build yet")
	}
}

// TestFieldAccessorsRunAgainstTheLibrary proves the accessors on a real shared
// object: the value the setter wrote is what the getter reads, including through
// the view of an opaque record field.
func TestFieldAccessorsRunAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/fields")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libfields.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "fields.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "fields")
	var report bytes.Buffer
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join(fixture, "fields.hpp")},
		Package: "fields",
		Library: library,
		Out:     out,
		Report:  &report,
	}))
	carrierOf(t, report.String(), "fields::Settings::count()", "thunk")

	shim := filepath.Join(out, ShimLibraryName("fields"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "fields_test.go"), []byte(fieldsE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./fields")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./fields"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./fields"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestFields")
		})
	}
}

// fieldsE2ESource is the program the generated fields package runs.
const fieldsE2ESource = `package fields

import (
	"os"
	"testing"
)

func TestFields(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}
	s, err := NewSettings()
	if err != nil {
		t.Fatalf("NewSettings: %v", err)
	}
	defer s.Close()

	// The scalar: the initialiser from the header, then the round trip.
	if got := s.Count(); got != 0 {
		t.Fatalf("Count = %v, want the header's 0", got)
	}
	s.SetCount(7)
	if got := s.Count(); got != 7 {
		t.Fatalf("Count after the write = %v", got)
	}

	// The enum field, with the enum's own Go type.
	if got := s.Kind(); got != KindPlain {
		t.Fatalf("Kind = %v, want Plain", got)
	}
	s.SetKind(KindFancy)
	if got := s.Kind(); got != KindFancy {
		t.Fatalf("Kind after the write = %v", got)
	}

	// The mirrored record field, by value.
	s.SetOrigin(Point{X: 1.5, Y: -2.5})
	if got := s.Origin(); got.X != 1.5 || got.Y != -2.5 {
		t.Fatalf("Origin = %v", got)
	}

	// The const field has no setter and keeps the declared value.
	if got := s.Version(); got != 2 {
		t.Fatalf("Version = %v", got)
	}

	// The opaque record field: the getter is a view of the same field, so a
	// write through the view is what the next read sees.
	view, err := s.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if got := view.Value(); got != 0 {
		t.Fatalf("Payload.Value = %v", got)
	}
	view.Set(11)
	again, err := s.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if got := again.Value(); got != 11 {
		t.Fatalf("Payload.Value after the write through the view = %v", got)
	}
	if err := view.Close(); err != nil {
		t.Fatalf("closing the view: %v", err)
	}
	if got := again.Value(); got != 11 {
		t.Fatalf("the object survives its view: %v", got)
	}

	// The setter copies from another object of the same type.
	other, err := NewPayload()
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	other.Set(42)
	if err := s.SetPayload(other); err != nil {
		t.Fatalf("SetPayload: %v", err)
	}
	final, err := s.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if got := final.Value(); got != 42 {
		t.Fatalf("Payload.Value after SetPayload = %v", got)
	}
}
`

// TestAMirroredRecordFieldIsAMirroredField covers the aggregate-of-aggregates
// shape: a record whose field is a record Go can mirror stays a Go struct with
// that field, rather than degrading to an accessor pair.
func TestAMirroredRecordFieldIsAMirroredField(t *testing.T) {
	header := writeHeader(t, "nest.hpp", `
namespace nest {
struct Inner { int x; };
struct Outer { Inner inner; int y; };
}
`)
	m := renderMap(t, Config{Headers: []string{header}, Package: "nest", Library: "libnest", Out: "."})
	source := m[GenFileName]
	assert.Contains(t, source, "type Inner struct {\n\tX int32\n}")
	assert.Contains(t, source, "type Outer struct {\n\tInner Inner\n\tY     int32\n}")
	assert.Contains(t, source, "var _ [0]byte = [unsafe.Offsetof(Outer{}.Inner)]byte{}")
	assert.NotContains(t, source, "func (o *Outer) Inner() Inner")
}
