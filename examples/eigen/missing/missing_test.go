//go:build cppgo_eigen

package missing_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	eigen "github.com/Manu343726/cppgo/examples/eigen"
)

// TestMain opens the same shim the other packages load. The shim does not
// contain the specialization this test names: it is built from the ones
// `make scan` collected, which are the ones in matrices/.
func TestMain(m *testing.M) {
	path := os.Getenv("CPPGO_EIGEN_SHIM")
	if path == "" {
		path = "../../libcppgo_eigen.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if _, err := os.Stat(path); err != nil {
		os.Exit(0) // no shim on this machine: the package cannot have been generated
	}
	if err := eigen.Open(path); err != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestMissingInstantiation is the two-phase contract's failure mode: a Go
// program instantiates a C++ specialization the last `cppgo build -scan` did
// not collect. The call must not reach a nil trampoline; it must name the C++
// specialization that is missing and say how to get it.
//
// The type is Matrix<double, 3, 3, 1, 3, 3> — row-major, three by three — which
// no other package here instantiates, so the shim has no thunk for it.
func TestMissingInstantiation(t *testing.T) {
	_, err := eigen.NewMatrix[float64, eigen.D3, eigen.D3, eigen.D1, eigen.D3, eigen.D3]()
	if err == nil {
		t.Fatal("NewMatrix[float64, D3, D3, D1, D3, D3] = nil error, want the missing-specialization error")
	}
	// The error names the C++ type the shim was not built for and the command
	// that would build it.
	if !strings.Contains(err.Error(), "::Eigen::Matrix<double, 3, 3, 1, 3, 3>") {
		t.Errorf("error does not name the missing specialization: %v", err)
	}
	if !strings.Contains(err.Error(), "cppgo build -scan") {
		t.Errorf("error does not name the command that collects it: %v", err)
	}
}
