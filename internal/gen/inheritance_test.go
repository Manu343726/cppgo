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

// TestInheritedMethodsAndBaseViews pins what a derived record carries: the
// base's public methods, called through the derived pointer so C++ applies the
// this adjustment, and As<Base>() / As<Derived>() as views of the same object.
func TestInheritedMethodsAndBaseViews(t *testing.T) {
	m := renderMap(t, fixtureConfig("inheritance", "inheritance.hpp"))
	source := m[GenFileName]
	shim := m[ShimSourceFileName]

	// Inherited methods, including the virtual one the base declares.
	assert.Contains(t, source, "func (r *Rect) Name() *int8 {")
	assert.Contains(t, source, "func (r *Rect) Id() int32 {")
	assert.Contains(t, source, "func (r *Rect) Draws() int32 {")
	assert.Contains(t, source, "func (r *Rect) Draw() {")
	assert.Contains(t, source, "func (r *Rect) Tag() int32 {")

	// The derived override keeps the derived declaration's body, and the base's
	// own Area is separate.
	assert.Contains(t, source, "func (r *Rect) Area() float64 {")
	assert.Contains(t, source, "func (s *Square) Area() float64 {")

	// The casts: up with static_cast, down with dynamic_cast and a bool.
	assert.Contains(t, source, "func (r *Rect) AsShape() *Shape {")
	assert.Contains(t, source, "func (r *Rect) AsDrawable() *Drawable {")
	assert.Contains(t, source, "func (r *Rect) AsTagged() *Tagged {")
	assert.Contains(t, source, "func (s *Square) AsShape() *Shape {")
	assert.Contains(t, source, "func (r *Rect) AsSquare() (*Square, bool) {")
	assert.Contains(t, source, "func (s *Shape) AsRect() (*Rect, bool) {")

	// The shim performs every cast, so Go never computes an offset: the base of
	// a multiple inheritance is at a non-zero offset and a virtual base is a
	// vtable lookup.
	assert.Contains(t, shim, "static_cast<::inherit::Shape *>(static_cast<::inherit::Rect *>(self))")
	assert.Contains(t, shim, "static_cast<::inherit::Drawable *>(static_cast<::inherit::Rect *>(self))")
	assert.Contains(t, shim, "static_cast<::inherit::Tagged *>(static_cast<::inherit::Rect *>(self))")
	assert.Contains(t, shim, "dynamic_cast<::inherit::Square *>(static_cast<::inherit::Rect *>(self))")

	// A non-polymorphic base cannot be dynamic_casted; the report says so
	// instead of emitting a cast that cannot compile.
	rendered, err := RenderResult(fixtureConfig("inheritance", "inheritance.hpp"))
	require.NoError(t, err)
	joined := ""
	for _, warning := range rendered.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "inherit::Flat is not polymorphic, so AsRaised is not generated")
	assert.Contains(t, source, "func (r *Raised) Level() int32 {")
}

// TestInheritanceRefusalIsGone is the negative half.
func TestInheritanceRefusalIsGone(t *testing.T) {
	outs, err := Render(fixtureConfig("inheritance", "inheritance.hpp"))
	require.NoError(t, err)
	for _, out := range outs {
		assert.NotContains(t, string(out.Data), "inherited members and base views are not emitted")
	}
}

// TestInheritanceRunsAgainstTheLibrary proves the adjustment at run time: a
// value reached as a base reads the derived override, and a base reached at a
// non-zero offset reads its own state.
func TestInheritanceRunsAgainstTheLibrary(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs("testdata/inheritance")
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libinherit.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "inheritance.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "inherit")
	var report bytes.Buffer
	cfg := fixtureConfig("inheritance", "inheritance.hpp")
	cfg.Package = "inherit"
	cfg.Headers = []string{filepath.Join(fixture, "inheritance.hpp")}
	cfg.Library = library
	cfg.Out = out
	cfg.Report = &report
	require.NoError(t, Generate(cfg))

	shim := filepath.Join(out, ShimLibraryName("inherit"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "inherit_test.go"), []byte(inheritE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./inherit")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./inherit"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./inherit"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestInheritance")
		})
	}
}

// inheritE2ESource is the program the generated inheritance package runs. Every
// assertion is a value the C++ produced through a base cast.
const inheritE2ESource = `package inherit

import (
	"os"
	"testing"
)

func TestInheritance(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	rect, err := NewRect()
	if err != nil {
		t.Fatalf("NewRect: %v", err)
	}

	// The base methods on the derived type, called through the derived pointer.
	if got := rect.Name(); got == nil || *got != 's' {
		t.Fatalf("Name = %v, want the first byte of \"shape\"", got)
	}
	if got := rect.Draws(); got != 0 {
		t.Fatalf("Draws = %v", got)
	}
	rect.Draw()
	if got := rect.Draws(); got != 1 {
		t.Fatalf("Draws after Draw = %v, want 1 through the Drawable subobject", got)
	}
	// The virtual base's state, reached through the vtable.
	if got := rect.Tag(); got != 7 {
		t.Fatalf("Tag = %v, want the virtual base's 7", got)
	}

	// The derived override is what a base view dispatches to.
	if got := rect.Area(); got != 4 {
		t.Fatalf("Rect.Area = %v, want 4", got)
	}
	asShape := rect.AsShape()
	if got := asShape.Area(); got != 4 {
		t.Fatalf("Shape.Area on a Rect = %v, want the Rect override 4", got)
	}

	// The base at a non-zero offset keeps its own state.
	asDrawable := rect.AsDrawable()
	asDrawable.Draw()
	if got := asDrawable.Draws(); got != 2 {
		t.Fatalf("Draws through the Drawable view = %v, want 2", got)
	}
	if got := rect.Draws(); got != 2 {
		t.Fatalf("Draws through the derived type after the view's call = %v, want 2", got)
	}

	// The virtual base view reads the same subobject.
	if got := rect.AsTagged().Tag(); got != 7 {
		t.Fatalf("Tag through the Tagged view = %v", got)
	}

	// A downcast that succeeds reports true; one that cannot is not attempted
	// by the type system, so the check here is the successful direction plus the
	// constant that a Rect is not a Square.
	if square, ok := rect.AsSquare(); ok || square != nil {
		t.Fatalf("a Rect reported itself as a Square: %v, %v", square, ok)
	}
	square, err := NewSquare()
	if err != nil {
		t.Fatalf("NewSquare: %v", err)
	}
	if got := square.Area(); got != 16 {
		t.Fatalf("Square.Area = %v, want 16", got)
	}
	asRectFromSquare := square.AsRect()
	if asRectFromSquare == nil {
		t.Fatal("a Square reached as a Rect must be a view")
	}
	if got := asRectFromSquare.Width(); got != 2 {
		t.Fatalf("Width through the Rect view = %v", got)
	}
	// The Square reached as a Shape still runs Square's override.
	if got := square.AsShape().Area(); got != 16 {
		t.Fatalf("Shape.Area on a Square = %v, want 16", got)
	}
}
`

// TestADirectBorrowedResultCoversTheRegisterPath pins the second view path: a
// method that is noexcept and defined out of line returns its address in the
// result register instead of through an out-pointer.
func TestADirectBorrowedResultCoversTheRegisterPath(t *testing.T) {
	m := renderMap(t, fixtureConfig("inheritance", "inheritance.hpp"))
	source := m[GenFileName]
	assert.Contains(t, source, "func (r *Rect) Identity() *Rect {")
	assert.Contains(t, source, "cppgoRet := cppgoCall_")
	assert.Contains(t, source, "return &Rect{obj: r.obj, addr: uintptr(cppgoRet)}")
}
