//go:build cppgo_opencv

package opencv

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
)

// The binding loads two objects: libcppgo_opencv.so, the shim cppgo compiled
// from the generated sources, and OpenCV's imgproc shared object, which that
// shim names. imgproc names core among its own dependencies, so both modules
// are loaded. The test names both, because the loader does not search the
// current directory.
//
// The library is the system's, so its path is a variable: a distribution puts
// libopencv_imgproc.so under its own libdir, and CPPGO_OPENCV_LIB is how the
// Makefile passes the one it found.
func init() {
	path := os.Getenv("CPPGO_OPENCV_LIB")
	if path == "" {
		path = "/usr/lib/x86_64-linux-gnu/libopencv_imgproc.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}

// shimPath is libcppgo_opencv.so: the shared object cppgo compiled from the
// generated shim and linked against libopencv_imgproc.so.
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
		fmt.Fprintf(os.Stderr, "opencv: skipping, libopencv_imgproc.so is missing: %v\n"+
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

// Mat type codes, from opencv2/core/hal/interface.h:
//
//	#define CV_8U   0
//	#define CV_8UC1 CV_MAKETYPE(CV_8U,1)   // 0
//	#define CV_8UC3 CV_MAKETYPE(CV_8U,3)   // 16
//
// CV_MAKETYPE(depth, channels) is depth + ((channels-1) << CV_CN_SHIFT) with
// CV_CN_SHIFT = 3, so one channel is 0 and three are 1<<3 = 8 above the depth
// (core/hal/interface.h, and the "Basic structures" page of OpenCV's
// documentation).
const (
	cv8UC1 int32 = 0
	cv8UC3 int32 = 16
)

// TestMatConstruction covers cv::Mat's carryable construction and its geometry:
// rows(), cols(), type(), total() and the field accessors generated for the
// public members. It also covers the empty matrix, which is what a caller gets
// before any image is loaded and what the error path below feeds to cvtColor.
func TestMatConstruction(t *testing.T) {
	m, err := NewMatInt32Int32Int32(2, 3, cv8UC3)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(2, 3, CV_8UC3): %v", err)
	}
	defer func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if got := m.Rows(); got != 2 {
		t.Errorf("Rows() = %d, want 2", got)
	}
	if got := m.Cols(); got != 3 {
		t.Errorf("Cols() = %d, want 3", got)
	}
	if got, err := m.Type(); err != nil || got != cv8UC3 {
		t.Errorf("Type() = %d, %v, want %d (CV_8UC3), nil", got, err, cv8UC3)
	}
	// total() is the product of the dimensions, counted in elements and not
	// bytes: 2*3 pixels of three channels are 6 elements (mat.hpp's total()).
	if got, err := m.Total(); err != nil || got != 6 {
		t.Errorf("Total() = %d, %v, want 6, nil", got, err)
	}
	if got := m.Dims(); got != 2 {
		t.Errorf("Dims() = %d, want 2", got)
	}
	if m.Data() == nil {
		t.Error("Data() of a 2x3 CV_8UC3 Mat is nil, want the allocated buffer")
	}

	// The default constructor makes the empty matrix: every dimension zero and
	// no buffer.
	empty, err := NewMat()
	if err != nil {
		t.Fatalf("NewMat: %v", err)
	}
	defer empty.Close()
	if got, err := empty.Total(); err != nil || got != 0 {
		t.Errorf("Total() of an empty Mat = %d, %v, want 0, nil", got, err)
	}
	if got := empty.Rows(); got != 0 {
		t.Errorf("Rows() of an empty Mat = %d, want 0", got)
	}
	if got := empty.Data(); got != nil {
		t.Errorf("Data() of an empty Mat = %v, want nil", got)
	}
}

// pixels is the buffer a cv::Mat owns, as a Go slice. data() is the generated
// accessor for the public uchar* member; a CV_8UC(n) matrix stores its pixels
// row-major with no padding because the constructor allocates it, so the buffer
// is rows*cols*channels bytes (the layout the Basic structures page documents).
// The slice does not own the memory: it is valid while the Mat is open.
func pixels(t *testing.T, m *Mat, n int) []byte {
	t.Helper()
	data := m.Data()
	if data == nil {
		t.Fatalf("Data() is nil for a %dx%d Mat", m.Rows(), m.Cols())
	}
	return unsafe.Slice(data, n)
}

// TestMatPixels is the element operation: a pixel is written through the
// matrix's buffer and read back, which is the round trip an image pipeline
// needs before any library function is called.
func TestMatPixels(t *testing.T) {
	m, err := NewMatInt32Int32Int32(1, 2, cv8UC3)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(1, 2, CV_8UC3): %v", err)
	}
	defer m.Close()

	// Two BGR pixels: red is (0, 0, 255) and green is (0, 255, 0), the channel
	// order cv::imread's COLOR_BGR2GRAY conversion expects.
	buf := pixels(t, m, 6)
	copy(buf, []byte{0, 0, 255, 0, 255, 0})

	for i, want := range []byte{0, 0, 255, 0, 255, 0} {
		if buf[i] != want {
			t.Errorf("pixel byte %d = %d, want %d", i, buf[i], want)
		}
	}
}

// TestSetTo fills every element of a matrix through cv::_InputArray's scalar
// constructor: setTo(InputArray value) assigns value to every channel of every
// pixel, so all nine bytes of a 1x3 CV_8UC3 matrix read back as 7.
func TestSetTo(t *testing.T) {
	m, err := NewMatInt32Int32Int32(1, 3, cv8UC3)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(1, 3, CV_8UC3): %v", err)
	}
	defer m.Close()

	value := 7.0
	scalar, err := NewInputArrayFloat64PtrRef(&value)
	if err != nil {
		t.Fatalf("NewInputArrayFloat64PtrRef: %v", err)
	}
	defer scalar.Close()

	if _, err := m.SetToInputArrayPtrRef(scalar); err != nil {
		t.Fatalf("SetTo(7): %v", err)
	}

	for i, got := range pixels(t, m, 9) {
		if got != 7 {
			t.Errorf("byte %d after SetTo(7) = %d, want 7", i, got)
		}
	}
}

// TestCvtColorBGR2GRAY converts two known BGR pixels to luminance. cvtColor is
// the enum-coded form: COLOR_BGR2GRAY is 6 in ColorConversionCodes
// (imgproc.hpp), and the conversion is
//
//	Y = 0.299 R + 0.587 G + 0.114 B
//
// which is the formula OpenCV's COLOR_BGR2GRAY documents (imgproc.hpp, the
// "Color conversions" section of the imgproc reference; the coefficients are
// BT.601's). Pure red is 0.299*255 = 76.245 and pure green is 0.587*255 =
// 149.685, rounded to 76 and 150 by the 8-bit conversion.
func TestCvtColorBGR2GRAY(t *testing.T) {
	if ColorConversionCodesCOLORBGR2GRAY != 6 {
		t.Errorf("COLOR_BGR2GRAY = %d, want 6", ColorConversionCodesCOLORBGR2GRAY)
	}

	src, err := NewMatInt32Int32Int32(1, 2, cv8UC3)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(1, 2, CV_8UC3): %v", err)
	}
	defer src.Close()
	copy(pixels(t, src, 6), []byte{0, 0, 255, 0, 255, 0})

	dst, err := NewMatInt32Int32Int32(1, 2, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(1, 2, CV_8UC1): %v", err)
	}
	defer dst.Close()

	in, err := NewInputArrayMatPtrRef(src)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()
	out, err := NewOutputArrayMatPtrRef(dst)
	if err != nil {
		t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
	}
	defer out.Close()

	if err := CvtColor(in, out, int32(ColorConversionCodesCOLORBGR2GRAY), 0); err != nil {
		t.Fatalf("CvtColor: %v", err)
	}

	// The destination is the Mat the OutputArray wraps: length runs the call, it
	// does not replace the object.
	if got := dst.Rows(); got != 1 {
		t.Errorf("dst Rows() after CvtColor = %d, want 1", got)
	}
	if got := dst.Cols(); got != 2 {
		t.Errorf("dst Cols() after CvtColor = %d, want 2", got)
	}
	gray := pixels(t, dst, 2)
	// 76.245 and 149.685 rounded to the nearest 8-bit value; the tolerance is
	// the rounding, not a fudge.
	if diff := math.Abs(float64(gray[0]) - 76.245); diff > 0.5 {
		t.Errorf("gray of pure red = %d, want 76 (0.299*255 = 76.245)", gray[0])
	}
	if diff := math.Abs(float64(gray[1]) - 149.685); diff > 0.5 {
		t.Errorf("gray of pure green = %d, want 150 (0.587*255 = 149.685)", gray[1])
	}
}

// TestNorm computes the L2 norm of a matrix through cv::norm. The four bytes
// are chosen so the answer is exact: 3^2 + 4^2 = 25, and sqrt(25) = 5.
// NormTypes::NORM_L2 is 4 in NormTypes (core/base.hpp).
func TestNorm(t *testing.T) {
	m, err := NewMatInt32Int32Int32(2, 2, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(2, 2, CV_8UC1): %v", err)
	}
	defer m.Close()
	copy(pixels(t, m, 4), []byte{3, 4, 0, 0})

	in, err := NewInputArrayMatPtrRef(m)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()
	empty, err := NewInputArray()
	if err != nil {
		t.Fatalf("NewInputArray: %v", err)
	}
	defer empty.Close()

	got, err := Norm(in, int32(NormTypesNORML2), empty)
	if err != nil {
		t.Fatalf("Norm: %v", err)
	}
	if !approx(got, 5) {
		t.Errorf("Norm(NORM_L2) of [3 4 0 0] = %v, want 5", got)
	}

	// The L1 norm of the same matrix is 3+4 = 7 (NormTypes::NORM_L1 = 2).
	got, err = Norm(in, int32(NormTypesNORML1), empty)
	if err != nil {
		t.Fatalf("Norm: %v", err)
	}
	if !approx(got, 7) {
		t.Errorf("Norm(NORM_L1) of [3 4 0 0] = %v, want 7", got)
	}
}

// TestCvtColorError is the library's own failure reaching Go: cvtColor on an
// empty source is invalid, OpenCV raises cv::Exception, and the thunk turns it
// into an error instead of letting it unwind into Go.
func TestCvtColorError(t *testing.T) {
	empty, err := NewMat()
	if err != nil {
		t.Fatalf("NewMat: %v", err)
	}
	defer empty.Close()
	dst, err := NewMatInt32Int32Int32(1, 1, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer dst.Close()

	in, err := NewInputArrayMatPtrRef(empty)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()
	out, err := NewOutputArrayMatPtrRef(dst)
	if err != nil {
		t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
	}
	defer out.Close()

	if err := CvtColor(in, out, int32(ColorConversionCodesCOLORBGR2GRAY), 0); err == nil {
		t.Error("CvtColor on an empty Mat returned nil, want the library's error")
	}
}

// approx compares two doubles with the tolerance the C++ side's own arithmetic
// needs: 0.25 and 0.5 are exact in binary, but the value crossed the boundary as
// a C++ double and the comparison says so rather than relying on it.
func approx(got, want float64) bool { return math.Abs(got-want) <= 1e-12 }