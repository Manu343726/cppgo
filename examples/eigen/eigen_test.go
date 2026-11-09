//go:build cppgo_eigen

package eigen

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Eigen is header-only, so there is one object to load: libcppgo_eigen.so, the
// shim cppgo compiled from the generated sources and the Eigen headers. There is
// no user library beside it, because the declarations are inline and the shim
// holds their code. The loader does not search the current directory, so the
// test names the file.
func shimPath() string {
	path := os.Getenv("CPPGO_EIGEN_SHIM")
	if path == "" {
		path = "libcppgo_eigen.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

// TestMain opens the generated binding.
func TestMain(m *testing.M) {
	if _, err := os.Stat(shimPath()); err != nil {
		fmt.Fprintf(os.Stderr, "eigen: skipping, the shim is missing: %v\n"+
			"run \"make\" in this directory first\n", err)
		os.Exit(0)
	}
	if err := Open(shimPath()); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestStorageOptions checks the storage-order and alignment enum against the
// values its declaration gives them. Eigen's Constants.h declares
//
//	enum StorageOptions {
//	  ColMajor = 0,
//	  RowMajor = 1,
//	  AutoAlign = 0,
//	  DontAlign = 2
//	};
//
// ColMajor and AutoAlign share a value because they are answers to different
// questions: the first says how elements are laid out, the second how the
// storage is aligned.
func TestStorageOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  StorageOptions
		want StorageOptions
	}{
		{"StorageOptionsColMajor", StorageOptionsColMajor, 0},
		{"StorageOptionsRowMajor", StorageOptionsRowMajor, 1},
		{"StorageOptionsAutoAlign", StorageOptionsAutoAlign, 0},
		{"StorageOptionsDontAlign", StorageOptionsDontAlign, 2},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestComputationInfo checks the status an Eigen algorithm reports: success
// first, then the three ways it can fail, in the order Constants.h declares
// them.
func TestComputationInfo(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  ComputationInfo
		want ComputationInfo
	}{
		{"ComputationInfoSuccess", ComputationInfoSuccess, 0},
		{"ComputationInfoNumericalIssue", ComputationInfoNumericalIssue, 1},
		{"ComputationInfoNoConvergence", ComputationInfoNoConvergence, 2},
		{"ComputationInfoInvalidInput", ComputationInfoInvalidInput, 3},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestUpLoType checks the triangular-view flags. The declaration in Constants.h
// builds the compound values the same way the generated constants spell them:
// UnitLower is Lower|UnitDiag, StrictlyUpper is Upper|ZeroDiag, and the
// self-adjoint and symmetric masks are powers of two above them.
func TestUpLoType(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  UpLoType
		want UpLoType
	}{
		{"UpLoTypeLower", UpLoTypeLower, 1},
		{"UpLoTypeUpper", UpLoTypeUpper, 2},
		{"UpLoTypeUnitDiag", UpLoTypeUnitDiag, 4},
		{"UpLoTypeZeroDiag", UpLoTypeZeroDiag, 8},
		{"UpLoTypeUnitLower", UpLoTypeUnitLower, 1 | 4},
		{"UpLoTypeUnitUpper", UpLoTypeUnitUpper, 2 | 4},
		{"UpLoTypeStrictlyLower", UpLoTypeStrictlyLower, 1 | 8},
		{"UpLoTypeStrictlyUpper", UpLoTypeStrictlyUpper, 2 | 8},
		{"UpLoTypeSelfAdjoint", UpLoTypeSelfAdjoint, 16},
		{"UpLoTypeSymmetric", UpLoTypeSymmetric, 32},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestThreads is the header-only case put to work: these three functions are
// inline, so their code is compiled into the shim and there is no shared object
// behind them.
//
// Eigen's Parallelizer.h defines nbThreads() as the thread count its parallel
// algorithms reserve, setNbThreads(int) as the setter, and initParallel() as the
// call a program makes before using Eigen from more than one thread. Without
// OpenMP the same file takes the `#else` branch and nbThreads() answers 1
// whatever was asked for, so the value after setNbThreads(3) is 3 with OpenMP
// and 1 without — and it is never something else.
func TestThreads(t *testing.T) {
	// initParallel() has no value to report; calling it proves the thunk runs.
	if err := InitParallel(); err != nil {
		t.Fatalf("InitParallel: %v", err)
	}

	before, err := NbThreads()
	if err != nil {
		t.Fatalf("NbThreads: %v", err)
	}
	if before < 1 {
		t.Fatalf("NbThreads() = %d, want at least 1", before)
	}

	if err := SetNbThreads(3); err != nil {
		t.Fatalf("SetNbThreads(3): %v", err)
	}
	got, err := NbThreads()
	if err != nil {
		t.Fatalf("NbThreads: %v", err)
	}
	if got != 3 && got != 1 {
		t.Errorf("NbThreads() after SetNbThreads(3) = %d, want 3 (OpenMP) or 1 (no OpenMP)", got)
	}

	// The value is process-wide, so the test puts back what it found.
	if err := SetNbThreads(before); err != nil {
		t.Fatalf("SetNbThreads(%d): %v", before, err)
	}
}
