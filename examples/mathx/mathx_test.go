package mathx

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Manu343726/cppgo"
)

// The generated package loads two objects: the shim cppgo built beside this
// package, and the user's library that shim was linked against. The dynamic
// loader does not search the current directory, so the test names both files.
//
// CppgoLibraryPath is read when the package is opened, which is why it is set
// in init before TestMain runs. The shim path goes straight to Open.
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

// shimPath is libcppgo_mathx.so: the shared object cppgo compiled from the
// generated shim and linked against libmathx.so.
func shimPath() string {
	path := os.Getenv("CPPGO_MATHX_SHIM")
	if path == "" {
		path = "libcppgo_mathx.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func TestMain(m *testing.M) {
	if _, err := os.Stat(CppgoLibraryPath); err != nil {
		fmt.Fprintf(os.Stderr, "libmathx.so is missing: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	if err := Open(shimPath()); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// approx compares doubles with a tolerance, because a norm coming out of
// std::sqrt is not required to be bit-identical to a Go computation of the same
// expression.
func approx(got, want float64) bool { return math.Abs(got-want) <= 1e-12 }

// TestConstructorAndAccessors covers the constructor and the accessors. X is
// defined inside the class body in the header, so it reaches C++ through the
// shim; the constructor and the destructor are shims too, because neither is
// noexcept.
func TestConstructorAndAccessors(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	defer func() {
		if err := v.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if got, err := v.X(); err != nil || got != 3 {
		t.Errorf("X() = %v, %v, want 3, nil", got, err)
	}
	if got, err := v.Y(); err != nil || got != 4 {
		t.Errorf("Y() = %v, %v, want 4, nil", got, err)
	}
	if got, err := VectorDims(); err != nil || got != 2 {
		t.Errorf("VectorDims() = %v, %v, want 2, nil", got, err)
	}
}

// TestConstMethod checks the value the const member computes, which is also the
// one the C++ side is responsible for: sqrt(3^2 + 4^2) = 5.
func TestConstMethod(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	defer v.Close()

	if got, err := v.Norm(); err != nil || !approx(got, 5) {
		t.Errorf("Norm() = %v, %v, want 5, nil", got, err)
	}

	// The zero vector, to show the arithmetic is really happening on the
	// object's own state rather than on something the test passed in.
	origin, err := NewVector(0, 0)
	if err != nil {
		t.Fatalf("NewVector(0, 0): %v", err)
	}
	defer origin.Close()
	if got, err := origin.Norm(); err != nil || got != 0 {
		t.Errorf("Norm() of the zero vector = %v, %v, want 0, nil", got, err)
	}
}

// TestDirectMethod is the one call the generator carries directly: scale is
// noexcept and defined out of line, so its mangled symbol is in the library
// and the Go signature has no error result.
func TestDirectMethod(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	defer v.Close()

	v.Scale(2)

	if got, err := v.X(); err != nil || got != 6 {
		t.Errorf("X() after Scale(2) = %v, %v, want 6, nil", got, err)
	}
	if got, err := v.Y(); err != nil || got != 8 {
		t.Errorf("Y() after Scale(2) = %v, %v, want 8, nil", got, err)
	}
	if got, err := v.Norm(); err != nil || !approx(got, 10) {
		t.Errorf("Norm() after Scale(2) = %v, %v, want 10, nil", got, err)
	}
}

// TestMethodTakingSlice is the (pointer, length) case: the slice goes in as a
// borrowed pointer and a length, with no copy.
func TestMethodTakingSlice(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	defer v.Close()

	// 3*2 + 4*1 = 10.
	if got, err := v.Dot([]float64{2, 1}); err != nil || !approx(got, 10) {
		t.Errorf("Dot([2 1]) = %v, %v, want 10, nil", got, err)
	}

	// A nil slice crosses as (nil, 0), and the C++ loop has nothing to read.
	if got, err := v.Dot(nil); err != nil || got != 0 {
		t.Errorf("Dot(nil) = %v, %v, want 0, nil", got, err)
	}

	// Longer than the vector: the C++ side clamps the length it reads, which is
	// the callee's contract to keep. 3*1 + 4*2 = 11, the 3 is ignored.
	if got, err := v.Dot([]float64{1, 2, 3}); err != nil || !approx(got, 11) {
		t.Errorf("Dot([1 2 3]) = %v, %v, want 11, nil", got, err)
	}
}

// TestThrowingMethod is the failure path: at() throws std::out_of_range, the
// shim catches it and stores the message, and the Go binding returns it as a
// *cppgo.CallError wrapping the text.
func TestThrowingMethod(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	defer v.Close()

	if got, err := v.At(0); err != nil || got != 3 {
		t.Errorf("At(0) = %v, %v, want 3, nil", got, err)
	}
	if got, err := v.At(1); err != nil || got != 4 {
		t.Errorf("At(1) = %v, %v, want 4, nil", got, err)
	}

	_, err = v.At(2)
	if err == nil {
		t.Fatal("At(2) returned no error")
	}
	if !strings.Contains(err.Error(), "index out of range") {
		t.Errorf("At(2) error = %q, want the message the C++ exception carried", err)
	}
	var call *cppgo.CallError
	if !errors.As(err, &call) {
		t.Fatalf("At(2) error is %T, want *cppgo.CallError", err)
	}
	if call.Symbol != "mathx::Vector::at" {
		t.Errorf("CallError.Symbol = %q, want mathx::Vector::at", call.Symbol)
	}
}

// TestPointByValue is the record case: Point is a 16-byte POD, so the Go value
// circulates by value and the shim copies it across the boundary in both
// directions.
func TestPointByValue(t *testing.T) {
	got, err := Midpoint(Point{X: 0, Y: 0}, Point{X: 4, Y: 8})
	if err != nil {
		t.Fatalf("Midpoint: %v", err)
	}
	if want := (Point{X: 2, Y: 4}); got != want {
		t.Errorf("Midpoint = %+v, want %+v", got, want)
	}
}

// TestSaxpy is the free-function slice case: two borrowed slices, and the
// generated binding refuses a length mismatch before the call ever happens.
func TestSaxpy(t *testing.T) {
	x := []float64{1, 2, 3}
	y := []float64{10, 20, 30}

	if err := Saxpy(2, x, y); err != nil {
		t.Fatalf("Saxpy: %v", err)
	}
	if want := []float64{12, 24, 36}; !equal(y, want) {
		t.Errorf("y after Saxpy = %v, want %v", y, want)
	}

	if err := Saxpy(1, []float64{1, 2}, []float64{3}); err == nil {
		t.Error("Saxpy with slices of different lengths returned no error")
	}
}

// TestObjectsAreIndependent covers the per-object storage the generated
// constructor allocates: two values, two objects, no shared state.
func TestObjectsAreIndependent(t *testing.T) {
	a, err := NewVector(1, 0)
	if err != nil {
		t.Fatalf("NewVector(1, 0): %v", err)
	}
	defer a.Close()

	b, err := NewVector(0, 2)
	if err != nil {
		t.Fatalf("NewVector(0, 2): %v", err)
	}
	defer b.Close()

	if got, err := a.Norm(); err != nil || !approx(got, 1) {
		t.Errorf("Norm() of the first object = %v, %v, want 1, nil", got, err)
	}
	if got, err := b.Norm(); err != nil || !approx(got, 2) {
		t.Errorf("Norm() of the second object = %v, %v, want 2, nil", got, err)
	}
}

// TestClone uses the copy constructor through the generated Clone: a second
// object, constructed from the first, independent of it.
func TestClone(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	defer v.Close()

	clone, err := v.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer clone.Close()

	v.Scale(2)
	if got, err := clone.X(); err != nil || got != 3 {
		t.Errorf("clone X() after the original was scaled = %v, %v, want 3, nil", got, err)
	}
}

// TestCloseIsIdempotentOnlyOnce is the ownership rule: Close destroys the
// object, and a second Close is reported rather than running the destructor
// twice. A method on a closed value is ErrClosed too, and the direct method,
// which has no error result, panics.
func TestCloseLifetime(t *testing.T) {
	v, err := NewVector(3, 4)
	if err != nil {
		t.Fatalf("NewVector(3, 4): %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := v.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Errorf("second Close = %v, want cppgo.ErrClosed", err)
	}
	if _, err := v.X(); !errors.Is(err, cppgo.ErrClosed) {
		t.Errorf("X() on a closed Vector = %v, want cppgo.ErrClosed", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("Scale on a closed Vector did not panic")
		}
	}()
	v.Scale(2)
}

func equal(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
