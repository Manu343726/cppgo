//go:build cppgo_eigen

package matrices_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Manu343726/cppgo"
	eigen "github.com/Manu343726/cppgo/examples/eigen"
	"github.com/Manu343726/cppgo/examples/eigen/matrices"
)

// The generated package opens one shim, and the subpackages that use it open
// the same one: libcppgo_eigen.so, whose path the Makefile passes in
// CPPGO_EIGEN_SHIM.
func init() {
	path := os.Getenv("CPPGO_EIGEN_SHIM")
	if path == "" {
		path = "../../libcppgo_eigen.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	shim = path
}

var shim string

// TestMain opens the shim: the concrete specializations these tests instantiate
// were collected into it by `make scan`, and without it the first call reports
// the missing counterpart.
func TestMain(m *testing.M) {
	if _, err := os.Stat(shim); err != nil {
		os.Exit(0) // no shim on this machine: the package cannot have been generated
	}
	if err := eigen.Open(shim); err != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestMatrix2d is the fixed-size specialization: a 2×2 row-major matrix of
// doubles, constructed by the two-phase generic API.
//
// The two values are Eigen's strides (DenseBase/MatrixBase's innerStride() and
// outerStride(), "the pointer increment between two consecutive entries within
// a given row of a row-major matrix" and "between two consecutive rows"): for a
// row-major matrix of doubles they are 1 element and 2 elements, the column
// count.
func TestMatrix2d(t *testing.T) {
	m, err := matrices.NewMatrix2d()
	if err != nil {
		t.Fatalf("NewMatrix2d: %v", err)
	}
	defer func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	inner, err := m.InnerStride()
	if err != nil {
		t.Fatalf("InnerStride: %v", err)
	}
	if inner != 1 {
		t.Errorf("InnerStride() of a row-major Matrix<double,2,2> = %d, want 1", inner)
	}
	outer, err := m.OuterStride()
	if err != nil {
		t.Fatalf("OuterStride: %v", err)
	}
	if outer != 2 {
		t.Errorf("OuterStride() of a row-major Matrix<double,2,2> = %d, want 2 (the column count)", outer)
	}
}

// TestMatrixXd is the dynamic specialization. A default-constructed dynamic
// matrix has no rows and no columns, so its outer stride is zero; the inner
// stride is one element, the unit increment of a row-major matrix.
func TestMatrixXd(t *testing.T) {
	m, err := matrices.NewMatrixXd()
	if err != nil {
		t.Fatalf("NewMatrixXd: %v", err)
	}
	defer m.Close()

	inner, err := m.InnerStride()
	if err != nil {
		t.Fatalf("InnerStride: %v", err)
	}
	if inner != 1 {
		t.Errorf("InnerStride() of a row-major MatrixXd = %d, want 1", inner)
	}
	outer, err := m.OuterStride()
	if err != nil {
		t.Fatalf("OuterStride: %v", err)
	}
	if outer != 0 {
		t.Errorf("OuterStride() of an empty MatrixXd = %d, want 0", outer)
	}
}

// TestCloseLifetime is the ownership rule: Close releases the object storage,
// a second Close is reported, and a method on a closed value returns
// cppgo.ErrClosed instead of calling into freed memory.
func TestCloseLifetime(t *testing.T) {
	m, err := matrices.NewMatrix2d()
	if err != nil {
		t.Fatalf("NewMatrix2d: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(); err != cppgo.ErrClosed {
		t.Errorf("second Close = %v, want cppgo.ErrClosed", err)
	}
	if _, err := m.InnerStride(); err != cppgo.ErrClosed {
		t.Errorf("InnerStride() on a closed Matrix = %v, want cppgo.ErrClosed", err)
	}
}
