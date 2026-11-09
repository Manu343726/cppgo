//go:build cppgo_opencv

package opencv

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Manu343726/cppgo"
)

// The binding loads two objects: libcppgo_opencv.so, the shim cppgo compiled
// from the generated sources, and OpenCV's core shared object, which that shim
// names. The test names both, because the loader does not search the current
// directory.
//
// The library is the system's, so its path is a variable: a distribution puts
// libopencv_core.so under its own libdir, and CPPGO_OPENCV_LIB is how the
// Makefile passes the one it found.
func init() {
	path := os.Getenv("CPPGO_OPENCV_LIB")
	if path == "" {
		path = "/usr/lib/x86_64-linux-gnu/libopencv_core.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}

// shimPath is libcppgo_opencv.so: the shared object cppgo compiled from the
// generated shim and linked against libopencv_core.so.
func shimPath() string {
	path := os.Getenv("CPPGO_OPENCV_SHIM")
	if path == "" {
		path = "libcppgo_opencv.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

// TestMain opens the generated binding. A machine without OpenCV skips the whole
// file with the reason, which is what the build tag does on a machine that
// cannot compile it and what this does on one that compiles but cannot load.
func TestMain(m *testing.M) {
	if _, err := os.Stat(CppgoLibraryPath); err != nil {
		fmt.Fprintf(os.Stderr, "opencv: skipping, libopencv_core.so is missing: %v\n"+
			"install libopencv-dev, or set CPPGO_OPENCV_LIB\n", err)
		os.Exit(0)
	}
	if err := Open(shimPath()); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestVersion reads the library's version out of the library itself. The two
// numbers are the macros CV_VERSION_MAJOR and CV_VERSION_MINOR that the
// installation's own version header defines, and the example is built against
// OpenCV 4.10 (the CI image's package).
func TestVersion(t *testing.T) {
	major, err := GetVersionMajor()
	if err != nil {
		t.Fatalf("GetVersionMajor: %v", err)
	}
	if major != 4 {
		t.Errorf("GetVersionMajor() = %d, want 4", major)
	}

	minor, err := GetVersionMinor()
	if err != nil {
		t.Fatalf("GetVersionMinor: %v", err)
	}
	if minor != 10 {
		t.Errorf("GetVersionMinor() = %d, want 10 (OpenCV 4.10)", minor)
	}
}

// TestNormTypes checks the enum's constants against the values its declaration
// gives them: NormTypes in opencv2/core/base.hpp is
//
//	enum NormTypes { NORM_INF = 1, NORM_L1 = 2, NORM_L2 = 4, NORM_L2SQR = 5,
//	                 NORM_HAMMING = 6, NORM_HAMMING2 = 7, NORM_TYPE_MASK = 7,
//	                 NORM_RELATIVE = 8, NORM_MINMAX = 32 };
//
// NORM_TYPE_MASK is the mask that keeps the type out of the flags, which is why
// it shares its value with NORM_HAMMING2: it is a mask, not a fourth norm.
func TestNormTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  NormTypes
		want NormTypes
	}{
		{"NormTypesNORMINF", NormTypesNORMINF, 1},
		{"NormTypesNORML1", NormTypesNORML1, 2},
		{"NormTypesNORML2", NormTypesNORML2, 4},
		{"NormTypesNORML2SQR", NormTypesNORML2SQR, 5},
		{"NormTypesNORMHAMMING", NormTypesNORMHAMMING, 6},
		{"NormTypesNORMHAMMING2", NormTypesNORMHAMMING2, 7},
		{"NormTypesNORMTYPEMASK", NormTypesNORMTYPEMASK, 7},
		{"NormTypesNORMRELATIVE", NormTypesNORMRELATIVE, 8},
		{"NormTypesNORMMINMAX", NormTypesNORMMINMAX, 32},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestTermCriteria covers the record case: a small struct with two public ints
// and a double, two constructors, a field accessor per member, and one method.
//
// The criteria type is documented in opencv2/core/types_c.h as
// COUNT = 1, MAX_ITER = COUNT, EPS = 2, and isValid() is documented to report
// whether the type is non-zero.
func TestTermCriteria(t *testing.T) {
	// COUNT and EPS together: the algorithm stops on either condition.
	both := int32(TermCriteriaTypeCOUNT) | int32(TermCriteriaTypeEPS)
	tc, err := NewTermCriteriaInt32Int32Float64(both, 5, 0.25)
	if err != nil {
		t.Fatalf("NewTermCriteriaInt32Int32Float64: %v", err)
	}
	defer func() {
		if err := tc.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if got := tc.Type(); got != both {
		t.Errorf("Type() = %d, want %d (COUNT|EPS)", got, both)
	}
	if got := tc.MaxCount(); got != 5 {
		t.Errorf("MaxCount() = %d, want 5", got)
	}
	if got := tc.Epsilon(); !approx(got, 0.25) {
		t.Errorf("Epsilon() = %v, want 0.25", got)
	}
	if got, err := tc.IsValid(); err != nil || !got {
		t.Errorf("IsValid() = %v, %v, want true, nil", got, err)
	}

	// The setters write the same fields the accessors read back.
	tc.SetMaxCount(11)
	tc.SetEpsilon(0.5)
	if got := tc.MaxCount(); got != 11 {
		t.Errorf("MaxCount() after SetMaxCount(11) = %d, want 11", got)
	}
	if got := tc.Epsilon(); !approx(got, 0.5) {
		t.Errorf("Epsilon() after SetEpsilon(0.5) = %v, want 0.5", got)
	}

	// The default constructor leaves the record zeroed, and a zero type is what
	// isValid() reports as invalid.
	zero, err := NewTermCriteria()
	if err != nil {
		t.Fatalf("NewTermCriteria: %v", err)
	}
	defer zero.Close()
	if got := zero.Type(); got != 0 {
		t.Errorf("Type() of the default TermCriteria = %d, want 0", got)
	}
	if got, err := zero.IsValid(); err != nil || got {
		t.Errorf("IsValid() of the default TermCriteria = %v, %v, want false, nil", got, err)
	}
}

// TestTickMeter covers the object with state: start and stop accumulate a run
// and a duration on the object, and reset clears both. The semantics are the
// ones documented beside the class in opencv2/core/utility.hpp: getCounter() is
// the number of start/stop pairs measured, and the time accessors sum the
// durations of those pairs.
func TestTickMeter(t *testing.T) {
	tm, err := NewTickMeter()
	if err != nil {
		t.Fatalf("NewTickMeter: %v", err)
	}
	defer func() {
		if err := tm.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if got, err := tm.GetCounter(); err != nil || got != 0 {
		t.Errorf("GetCounter() of a fresh meter = %d, %v, want 0, nil", got, err)
	}
	if got, err := tm.GetTimeTicks(); err != nil || got != 0 {
		t.Errorf("GetTimeTicks() of a fresh meter = %d, %v, want 0, nil", got, err)
	}

	if err := tm.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tm.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got, err := tm.GetCounter(); err != nil || got != 1 {
		t.Errorf("GetCounter() after one start/stop = %d, %v, want 1, nil", got, err)
	}
	ticks, err := tm.GetTimeTicks()
	if err != nil {
		t.Fatalf("GetTimeTicks: %v", err)
	}
	if ticks < 0 {
		t.Errorf("GetTimeTicks() = %d, want a non-negative count of ticks", ticks)
	}
	sec, err := tm.GetTimeSec()
	if err != nil {
		t.Fatalf("GetTimeSec: %v", err)
	}
	if sec < 0 {
		t.Errorf("GetTimeSec() = %v, want a non-negative duration", sec)
	}
	// getTimeSec() is getTimeTicks() divided by getTickFrequency(), so the two
	// must agree to the precision of the division.
	freq, err := GetTickFrequency()
	if err != nil {
		t.Fatalf("GetTickFrequency: %v", err)
	}
	if want := float64(ticks) / freq; math.Abs(sec-want) > 1e-9 {
		t.Errorf("GetTimeSec() = %v, want getTimeTicks()/getTickFrequency() = %v", sec, want)
	}
	// The meter measured one run in a non-zero number of ticks, so the rate it
	// reports is positive and finite.
	fps, err := tm.GetFPS()
	if err != nil {
		t.Fatalf("GetFPS: %v", err)
	}
	if !(fps > 0) || math.IsInf(fps, 0) {
		t.Errorf("GetFPS() = %v, want a positive finite rate", fps)
	}

	// A second run is counted, not replaced.
	if err := tm.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tm.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got, err := tm.GetCounter(); err != nil || got != 2 {
		t.Errorf("GetCounter() after two start/stop pairs = %d, %v, want 2, nil", got, err)
	}
	if got, err := tm.GetAvgTimeSec(); err != nil || got < 0 {
		t.Errorf("GetAvgTimeSec() = %v, %v, want a non-negative average", got, err)
	}

	if err := tm.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got, err := tm.GetCounter(); err != nil || got != 0 {
		t.Errorf("GetCounter() after Reset = %d, %v, want 0, nil", got, err)
	}
	if got, err := tm.GetTimeTicks(); err != nil || got != 0 {
		t.Errorf("GetTimeTicks() after Reset = %d, %v, want 0, nil", got, err)
	}
}

// TestRuntimeConfiguration covers the free functions: values the library reports
// about its own build and about the process it is running in. The meanings are
// the ones documented beside each declaration in opencv2/core/utility.hpp.
func TestRuntimeConfiguration(t *testing.T) {
	freq, err := GetTickFrequency()
	if err != nil {
		t.Fatalf("GetTickFrequency: %v", err)
	}
	if !(freq > 0) {
		t.Errorf("GetTickFrequency() = %v, want a positive number of ticks per second", freq)
	}

	cpus, err := GetNumberOfCPUs()
	if err != nil {
		t.Fatalf("GetNumberOfCPUs: %v", err)
	}
	if cpus < 1 {
		t.Errorf("GetNumberOfCPUs() = %d, want at least 1", cpus)
	}

	// setNumThreads(n) sets the thread count getNumThreads reports, and the two
	// round-trip. The value is process-wide, so the test puts back what it found.
	before, err := GetNumThreads()
	if err != nil {
		t.Fatalf("GetNumThreads: %v", err)
	}
	if err := SetNumThreads(2); err != nil {
		t.Fatalf("SetNumThreads(2): %v", err)
	}
	if got, err := GetNumThreads(); err != nil || got != 2 {
		t.Errorf("GetNumThreads() after SetNumThreads(2) = %d, %v, want 2, nil", got, err)
	}
	if err := SetNumThreads(before); err != nil {
		t.Fatalf("SetNumThreads(%d): %v", before, err)
	}

	// setUseOptimized toggles the dispatch to the library's optimized code, and
	// useOptimized reads the flag back.
	beforeOptimized, err := UseOptimized()
	if err != nil {
		t.Fatalf("UseOptimized: %v", err)
	}
	if err := SetUseOptimized(false); err != nil {
		t.Fatalf("SetUseOptimized(false): %v", err)
	}
	if got, err := UseOptimized(); err != nil || got {
		t.Errorf("UseOptimized() after SetUseOptimized(false) = %v, %v, want false, nil", got, err)
	}
	if err := SetUseOptimized(true); err != nil {
		t.Fatalf("SetUseOptimized(true): %v", err)
	}
	if got, err := UseOptimized(); err != nil || !got {
		t.Errorf("UseOptimized() after SetUseOptimized(true) = %v, %v, want true, nil", got, err)
	}
	if err := SetUseOptimized(beforeOptimized); err != nil {
		t.Fatalf("SetUseOptimized(%v): %v", beforeOptimized, err)
	}
}

// TestCloseLifetime is the ownership rule the generated records share: Close
// destroys the C++ object, a second Close is reported, and a method on a closed
// value returns ErrClosed instead of calling into freed memory.
func TestCloseLifetime(t *testing.T) {
	tm, err := NewTickMeter()
	if err != nil {
		t.Fatalf("NewTickMeter: %v", err)
	}
	if err := tm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := tm.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Errorf("second Close = %v, want cppgo.ErrClosed", err)
	}
	if _, err := tm.GetCounter(); !errors.Is(err, cppgo.ErrClosed) {
		t.Errorf("GetCounter() on a closed TickMeter = %v, want cppgo.ErrClosed", err)
	}

	// A record whose fields are accessors is closed the same way, and its
	// accessors report the closed object rather than reading a freed one.
	tc, err := NewTermCriteria()
	if err != nil {
		t.Fatalf("NewTermCriteria: %v", err)
	}
	if err := tc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := tc.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Errorf("second Close = %v, want cppgo.ErrClosed", err)
	}
}

// approx compares two doubles with the tolerance the C++ side's own arithmetic
// needs: 0.25 and 0.5 are exact in binary, but the value crossed the boundary as
// a C++ double and the comparison says so rather than relying on it.
func approx(got, want float64) bool { return math.Abs(got-want) <= 1e-12 }
