package mathx

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Manu343726/cppgo"
)

// The generated bindings open the library named by //cppgo:library ("libmathx")
// through the dynamic loader, which does not search the current directory, so
// the test has to point them at the file the Makefile built. CppgoLibraryPath
// is read once, on the first call, which is why this runs in init.
func init() {
	path := os.Getenv("CPPGO_MATHX_LIB")
	if path == "" {
		path = filepath.Join("build", "libmathx.so")
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}

func TestMain(m *testing.M) {
	if _, err := os.Stat(CppgoLibraryPath); err != nil {
		fmt.Fprintf(os.Stderr, "libmathx.so is missing: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// approx compares doubles with a tolerance, because a norm coming out of
// std::sqrt is not required to be bit-identical to a Go computation of the same
// expression.
func approx(got, want float64) bool { return math.Abs(got-want) <= 1e-12 }

// TestConstructorAndAccessors covers the constructor and the two accessors: a
// `this` in RDI with the doubles in XMM0/XMM1, and a const member returning in
// XMM0.
func TestConstructorAndAccessors(t *testing.T) {
	v, err := New(3, 4)
	if err != nil {
		t.Fatalf("New(3, 4): %v", err)
	}
	if cppgo.Handle(v).IsNil() {
		t.Fatal("New returned a null handle")
	}

	if got := v.X(); got != 3 {
		t.Errorf("X() = %v, want 3", got)
	}
	if got := v.Y(); got != 4 {
		t.Errorf("Y() = %v, want 4", got)
	}
	if got := Dims(); got != 2 {
		t.Errorf("Dims() = %v, want 2", got)
	}

	v.Destroy()
}

// TestConstMethod checks the value the const member computes, which is also the
// one the C++ side is responsible for: sqrt(3² + 4²) = 5.
func TestConstMethod(t *testing.T) {
	v, err := New(3, 4)
	if err != nil {
		t.Fatalf("New(3, 4): %v", err)
	}
	defer v.Destroy()

	if got := v.Norm(); !approx(got, 5) {
		t.Errorf("Norm() = %v, want 5", got)
	}

	// The zero vector, to show the arithmetic is really happening on the
	// object's own state rather than on something the test passed in.
	origin, err := New(0, 0)
	if err != nil {
		t.Fatalf("New(0, 0): %v", err)
	}
	defer origin.Destroy()
	if got := origin.Norm(); got != 0 {
		t.Errorf("Norm() of the zero vector = %v, want 0", got)
	}
}

// TestMethodTakingSlice is the (pointer, length) case: the slice goes in as
// RSI/RDX right after `this` in RDI.
func TestMethodTakingSlice(t *testing.T) {
	v, err := New(3, 4)
	if err != nil {
		t.Fatalf("New(3, 4): %v", err)
	}
	defer v.Destroy()

	// 3*2 + 4*1 = 10.
	if got := v.Dot([]float64{2, 1}); !approx(got, 10) {
		t.Errorf("Dot([2 1]) = %v, want 10", got)
	}

	// A nil slice crosses as (nil, 0): cppgo.Ptr returns nil for an empty
	// slice, and the C++ loop has nothing to read.
	if got := v.Dot(nil); got != 0 {
		t.Errorf("Dot(nil) = %v, want 0", got)
	}

	// Longer than the vector: the C++ side clamps the length it reads, which
	// is the callee's contract to keep. 3*1 + 4*2 = 11, the 3 is ignored.
	if got := v.Dot([]float64{1, 2, 3}); !approx(got, 11) {
		t.Errorf("Dot([1 2 3]) = %v, want 11", got)
	}
}

// TestMethodMutatesObject proves the handle is the object: a non-const member
// called through it changes what a later const member sees.
func TestMethodMutatesObject(t *testing.T) {
	v, err := New(3, 4)
	if err != nil {
		t.Fatalf("New(3, 4): %v", err)
	}
	defer v.Destroy()

	v.Scale(2)

	if got := v.X(); got != 6 {
		t.Errorf("X() after Scale(2) = %v, want 6", got)
	}
	if got := v.Y(); got != 8 {
		t.Errorf("Y() after Scale(2) = %v, want 8", got)
	}
	if got := v.Norm(); !approx(got, 10) {
		t.Errorf("Norm() after Scale(2) = %v, want 10", got)
	}
}

// TestObjectsAreIndependent covers the per-object storage the generated
// constructor allocates: two handles, two objects, no shared state.
func TestObjectsAreIndependent(t *testing.T) {
	a, err := New(1, 0)
	if err != nil {
		t.Fatalf("New(1, 0): %v", err)
	}
	defer a.Destroy()

	b, err := New(0, 2)
	if err != nil {
		t.Fatalf("New(0, 2): %v", err)
	}
	defer b.Destroy()

	if a == b {
		t.Fatal("two constructions returned the same handle")
	}
	if got := a.Norm(); !approx(got, 1) {
		t.Errorf("Norm() of the first object = %v, want 1", got)
	}
	if got := b.Norm(); !approx(got, 2) {
		t.Errorf("Norm() of the second object = %v, want 2", got)
	}
}

// TestDestroyReleasesStorage checks the other half of the lifetime: the Go
// storage a constructor allocated (the handle is a uintptr, so nothing else
// keeps it alive) is dropped when the destructor runs.
func TestDestroyReleasesStorage(t *testing.T) {
	baseline := liveObjects()

	v, err := New(1, 1)
	if err != nil {
		t.Fatalf("New(1, 1): %v", err)
	}
	if got := liveObjects(); got != baseline+1 {
		t.Fatalf("%d objects registered after New, want %d", got, baseline+1)
	}

	v.Destroy()

	if got := liveObjects(); got != baseline {
		t.Errorf("%d objects still registered after Destroy, want %d", got, baseline)
	}
}

// liveObjects counts the entries in the storage registry the generated
// constructor allocates from. It is unexported state of the generated file, and
// reaching into it is the only way to see the release from the outside.
func liveObjects() int {
	cppgoObjectsMu.Lock()
	defer cppgoObjectsMu.Unlock()
	return len(cppgoObjects)
}
