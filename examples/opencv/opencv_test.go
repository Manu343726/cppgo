//go:build cppgo_opencv

package opencv

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
)

// The binding loads two objects: libcppgo_opencv.so, the shim cppgo compiled
// from the generated sources, and OpenCV's imgcodecs shared object, which that
// shim names. imgcodecs names imgproc and core among its own dependencies, so
// all three modules are loaded. The test names both, because the loader does
// not search the current directory.
//
// The library is the system's, so its path is a variable: a distribution puts
// libopencv_imgcodecs.so under its own libdir, and CPPGO_OPENCV_LIB is how the
// Makefile passes the one it found.
func init() {
	path := os.Getenv("CPPGO_OPENCV_LIB")
	if path == "" {
		path = "/usr/lib/x86_64-linux-gnu/libopencv_imgcodecs.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}

// shimPath is libcppgo_opencv.so: the shared object cppgo compiled from the
// generated shim and linked against libopencv_imgcodecs.so.
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
		fmt.Fprintf(os.Stderr, "opencv: skipping, libopencv_imgcodecs.so is missing: %v\n"+
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
	cv64F  int32 = 6
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

// decodeImage loads a real PNG through cv::imread and fails the test when the
// codec refuses it.
func decodeImage(t *testing.T, path string) *Mat {
	t.Helper()
	m, err := ImreadString(path)
	if err != nil {
		t.Fatalf("ImreadString(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if rows := m.Rows(); rows == 0 {
		t.Fatalf("ImreadString(%q) returned an empty matrix", path)
	}
	return m
}

// bgr is the matrix's pixels as one byte slice: a CV_8UC(n) matrix stores its
// pixels row-major with no padding, so the buffer is rows*cols*channels bytes
// (the layout the Basic structures page documents, and the one the constructor
// allocates).
func bgr(t *testing.T, m *Mat) []byte {
	t.Helper()
	channels, err := m.Channels()
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	n := int(m.Rows()) * int(m.Cols()) * int(channels)
	data := m.Data()
	if data == nil {
		t.Fatalf("Data() is nil for a %dx%d matrix", m.Rows(), m.Cols())
	}
	return unsafe.Slice(data, n)
}

// fixture is the committed 4x3 BGR image, one pixel per entry.
const fixture = "testdata/pixels.png"

// fixturePixels is the image's content as imread reads it: BGR bytes, row
// major. The PNG is written from these values by the fixture itself.
var fixturePixels = [][3]byte{
	{0, 0, 255}, {0, 255, 0}, {255, 0, 0}, {255, 255, 255},
	{0, 0, 0}, {128, 128, 128}, {0, 255, 255}, {255, 255, 0},
	{255, 0, 255}, {10, 20, 30}, {40, 50, 60}, {70, 80, 90},
}

// TestImread reads the fixture and asserts every pixel: the geometry
// (rows, cols, channels, type) and the twelve BGR triples.
func TestImread(t *testing.T) {
	m := decodeImage(t, fixture)

	if got := m.Rows(); got != 3 {
		t.Errorf("Rows() = %d, want 3", got)
	}
	if got := m.Cols(); got != 4 {
		t.Errorf("Cols() = %d, want 4", got)
	}
	if got, err := m.Channels(); err != nil || got != 3 {
		t.Errorf("Channels() = %d, %v, want 3, nil", got, err)
	}
	if got, err := m.Type(); err != nil || got != cv8UC3 {
		t.Errorf("Type() = %d, %v, want %d (CV_8UC3), nil", got, err, cv8UC3)
	}
	if got, err := m.Total(); err != nil || got != 12 {
		t.Errorf("Total() = %d, %v, want 12, nil", got, err)
	}
	if got, err := m.Empty(); err != nil || got {
		t.Errorf("Empty() = %v, %v, want false, nil", got, err)
	}

	pixels := bgr(t, m)
	for i, want := range fixturePixels {
		got := pixels[i*3 : i*3+3]
		if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("pixel %d = BGR%v, want BGR%v", i, got, want)
		}
	}
}

// TestImwriteRoundTrip is the other half of the codec path: the matrix read
// from the fixture is written to a temp directory and read back, and every
// pixel survives the PNG round trip byte for byte (PNG is lossless).
func TestImwriteRoundTrip(t *testing.T) {
	src := decodeImage(t, fixture)
	in, err := NewInputArrayMatPtrRef(src)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()

	out := filepath.Join(t.TempDir(), "copy.png")
	ok, err := ImwriteStringInputArrayPtrRef(out, in)
	if err != nil || !ok {
		t.Fatalf("ImwriteStringInputArrayPtrRef(%q) = %v, %v, want true, nil", out, ok, err)
	}

	back := decodeImage(t, out)
	if got, want := bgr(t, back), bgr(t, src); string(got) != string(want) {
		t.Errorf("the round trip changed the pixels:\n got %v\nwant %v", got, want)
	}
}

// TestImencode is the in-memory codec path: the same matrix encoded as PNG
// through the std::vector<uchar> out-parameter, whose bytes start with the PNG
// signature (89 50 4E 47 0D 0A 1A 0A, the format's own first eight bytes).
func TestImencode(t *testing.T) {
	src := decodeImage(t, fixture)
	in, err := NewInputArrayMatPtrRef(src)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()

	var buffer []byte
	ok, err := ImencodeStringInputArrayPtrRefUint8Slice(".png", in, &buffer)
	if err != nil || !ok {
		t.Fatalf("Imencode(.png) = %v, %v, want true, nil", ok, err)
	}
	signature := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if len(buffer) < len(signature) || string(buffer[:len(signature)]) != string(signature) {
		t.Fatalf("encoded %d bytes with prefix %x, want the PNG signature %x", len(buffer), buffer[:min(len(buffer), 8)], signature)
	}
}

// TestMatElementAccess is element access through the instantiated at<uchar>
// and at<double> templates: the reference the C++ side returns is the byte (or
// double) in the matrix, and a write through it changes the matrix.
func TestMatElementAccess(t *testing.T) {
	gray, err := NewMatInt32Int32Int32(2, 2, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(2, 2, CV_8UC1): %v", err)
	}
	defer gray.Close()
	copy(bgr(t, gray), []byte{3, 4, 0, 0})

	for _, tc := range []struct {
		row, col int32
		want     byte
	}{{0, 0, 3}, {0, 1, 4}, {1, 0, 0}, {1, 1, 0}} {
		value, err := gray.AtUint8(tc.row, tc.col)
		if err != nil {
			t.Fatalf("AtUint8(%d, %d): %v", tc.row, tc.col, err)
		}
		if *value != tc.want {
			t.Errorf("AtUint8(%d, %d) = %d, want %d", tc.row, tc.col, *value, tc.want)
		}
	}

	// The reference is writable: at<uchar> returns uchar&, and assigning through
	// it is the pixel write the C++ API offers.
	value, err := gray.AtUint8(1, 1)
	if err != nil {
		t.Fatalf("AtUint8: %v", err)
	}
	*value = 9
	if got := bgr(t, gray)[3]; got != 9 {
		t.Errorf("pixel (1, 1) after writing through at = %d, want 9", got)
	}

	// The double instantiation reads a CV_64F matrix at its element width.
	real, err := NewMatInt32Int32Int32(1, 2, cv64F)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32(1, 2, CV_64F): %v", err)
	}
	defer real.Close()
	doubles := unsafe.Slice((*float64)(unsafe.Pointer(real.Data())), 2)
	doubles[0], doubles[1] = 1.5, -2.25
	first, err := real.AtFloat64(0, 0)
	if err != nil {
		t.Fatalf("AtFloat64(0, 0): %v", err)
	}
	if *first != 1.5 {
		t.Errorf("AtFloat64(0, 0) = %v, want 1.5", *first)
	}
	second, err := real.AtFloat64(0, 1)
	if err != nil {
		t.Fatalf("AtFloat64(0, 1): %v", err)
	}
	if *second != -2.25 {
		t.Errorf("AtFloat64(0, 1) = %v, want -2.25", *second)
	}
}

// TestMatCopyAndClone covers the two copying paths: Clone is the deep copy
// cv::Mat::clone makes (an independent buffer), and Copy is the copy
// constructor, which shares the buffer and bumps its reference count.
func TestMatCopyAndClone(t *testing.T) {
	src, err := NewMatInt32Int32Int32(1, 2, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer src.Close()
	copy(bgr(t, src), []byte{1, 2})

	clone, err := src.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer clone.Close()
	// Writing into the clone must not change the source: clone() allocates.
	bgr(t, clone)[0] = 42
	if got := bgr(t, src)[0]; got != 1 {
		t.Errorf("source after writing the clone = %d, want 1 (clone is independent)", got)
	}

	shared, err := src.Copy()
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	defer shared.Close()
	// The copy constructor shares the data buffer, so a write through one is
	// visible through the other until one of them reallocates.
	bgr(t, shared)[0] = 7
	if got := bgr(t, src)[0]; got != 7 {
		t.Errorf("source after writing the shallow copy = %d, want 7 (Copy shares the buffer)", got)
	}
}

// TestMatSetToConvertReshape covers the three shape/value operations: setTo
// fills every channel, convertTo changes the element type (and applies the
// linear scale), and reshape changes the channel count of the same data.
func TestMatSetToConvertReshape(t *testing.T) {
	m, err := NewMatInt32Int32Int32(2, 2, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
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
	for i, got := range bgr(t, m) {
		if got != 7 {
			t.Errorf("byte %d after SetTo(7) = %d, want 7", i, got)
		}
	}

	// convertTo(CV_64F, alpha=0.5): every byte 7 becomes the double 3.5.
	dst, err := NewMat()
	if err != nil {
		t.Fatalf("NewMat: %v", err)
	}
	defer dst.Close()
	out, err := NewOutputArrayMatPtrRef(dst)
	if err != nil {
		t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
	}
	defer out.Close()
	if err := m.ConvertTo(out, cv64F, 0.5, 0); err != nil {
		t.Fatalf("ConvertTo: %v", err)
	}
	if got, err := dst.Type(); err != nil || got != cv64F {
		t.Errorf("converted Type() = %d, %v, want %d (CV_64F), nil", got, err, cv64F)
	}
	doubles := unsafe.Slice((*float64)(unsafe.Pointer(dst.Data())), 4)
	for i, got := range doubles {
		if got != 3.5 {
			t.Errorf("converted element %d = %v, want 3.5", i, got)
		}
	}

	// reshape(1) of a 2x2 single-channel matrix is the same 4 elements; the
	// interesting case is a 1x4 single-channel matrix reshaped to 1x2 two-channel.
	flat, err := NewMatInt32Int32Int32(1, 4, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer flat.Close()
	copy(bgr(t, flat), []byte{1, 2, 3, 4})
	reshaped, err := flat.ReshapeInt32(2)
	if err != nil {
		t.Fatalf("ReshapeInt32(2): %v", err)
	}
	defer reshaped.Close()
	if got, err := reshaped.Channels(); err != nil || got != 2 {
		t.Errorf("reshaped Channels() = %d, %v, want 2, nil", got, err)
	}
	if got := reshaped.Cols(); got != 2 {
		t.Errorf("reshaped Cols() = %d, want 2", got)
	}
}

// TestCvtColorPixelwise converts the fixture to grayscale and asserts every
// pixel against OpenCV's own 8-bit formula:
//
//	Y = (B*B2Y + G*G2Y + R*R2Y + (1 << (yuv_shift-1))) >> yuv_shift
//
// with R2Y = 4899, G2Y = 9617, B2Y = 1868 and yuv_shift = 14, the fixed-point
// coefficients of OpenCV 4.10's color_rgb.simd.hpp (the source of
// COLOR_BGR2GRAY; the values are BT.601's, rounded to 14 bits).
func TestCvtColorPixelwise(t *testing.T) {
	src := decodeImage(t, fixture)
	in, err := NewInputArrayMatPtrRef(src)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()

	dst, err := NewMat()
	if err != nil {
		t.Fatalf("NewMat: %v", err)
	}
	defer dst.Close()
	out, err := NewOutputArrayMatPtrRef(dst)
	if err != nil {
		t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
	}
	defer out.Close()

	if err := CvtColor(in, out, int32(ColorConversionCodesCOLORBGR2GRAY), 0); err != nil {
		t.Fatalf("CvtColor: %v", err)
	}
	if got, err := dst.Channels(); err != nil || got != 1 {
		t.Fatalf("gray Channels() = %d, %v, want 1, nil", got, err)
	}

	gray := bgr(t, dst)
	for i, pixel := range fixturePixels {
		want := (int(pixel[0])*1868 + int(pixel[1])*9617 + int(pixel[2])*4899 + (1 << 13)) >> 14
		if int(gray[i]) != want {
			t.Errorf("gray pixel %d = %d, want %d for BGR%v", i, gray[i], want, pixel)
		}
	}
}

// TestThreshold is the enum-coded fixed-level threshold: THRESH_BINARY turns
// everything above the level into maxval and everything else into zero, and
// THRESH_BINARY_INV does the opposite.
func TestThreshold(t *testing.T) {
	src, err := NewMatInt32Int32Int32(1, 4, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer src.Close()
	copy(bgr(t, src), []byte{0, 127, 128, 255})

	for _, tc := range []struct {
		kind int32
		want []byte
	}{
		{int32(ThresholdTypesTHRESHBINARY), []byte{0, 0, 255, 255}},
		{int32(ThresholdTypesTHRESHBINARYINV), []byte{255, 255, 0, 0}},
	} {
		in, err := NewInputArrayMatPtrRef(src)
		if err != nil {
			t.Fatalf("NewInputArrayMatPtrRef: %v", err)
		}
		dst, err := NewMat()
		if err != nil {
			t.Fatalf("NewMat: %v", err)
		}
		out, err := NewOutputArrayMatPtrRef(dst)
		if err != nil {
			t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
		}
		// threshold returns the level it used. For an 8-bit source OpenCV
		// truncates it to the element type (thresh.cpp converts the level to
		// the array's depth before comparing), so 127.5 comes back as 127.
		level, err := Threshold(in, out, 127.5, 255, tc.kind)
		if err != nil {
			t.Fatalf("Threshold: %v", err)
		}
		if level != 127 {
			t.Errorf("Threshold returned %v, want 127 (127.5 truncated to CV_8U)", level)
		}
		if got, want := bgr(t, dst), tc.want; string(got) != string(want) {
			t.Errorf("threshold kind %d = %v, want %v", tc.kind, got, want)
		}
		in.Close()
		out.Close()
		dst.Close()
	}
}

// TestResize duplicates a 2x2 matrix to 4x4 with INTER_NEAREST, so every source
// pixel is exactly four destination pixels.
func TestResize(t *testing.T) {
	src, err := NewMatInt32Int32Int32(2, 2, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer src.Close()
	copy(bgr(t, src), []byte{1, 2, 3, 4})

	in, err := NewInputArrayMatPtrRef(src)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()
	dst, err := NewMat()
	if err != nil {
		t.Fatalf("NewMat: %v", err)
	}
	defer dst.Close()
	out, err := NewOutputArrayMatPtrRef(dst)
	if err != nil {
		t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
	}
	defer out.Close()

	if err := Resize(in, out, SizeInt{Width: 4, Height: 4}, 0, 0, int32(InterpolationFlagsINTERNEAREST)); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if got := dst.Rows(); got != 4 {
		t.Errorf("resized Rows() = %d, want 4", got)
	}
	want := []byte{
		1, 1, 2, 2,
		1, 1, 2, 2,
		3, 3, 4, 4,
		3, 3, 4, 4,
	}
	if got := bgr(t, dst); string(got) != string(want) {
		t.Errorf("nearest-neighbour resize = %v, want %v", got, want)
	}
}

// TestBlur keeps a constant image constant: a box filter and a Gaussian float
// preserve a uniform field, so every output pixel is the input value. It is an
// exact assertion — any weighting error shows as a different byte — and it is
// what proves the Size and Point records cross by value.
func TestBlur(t *testing.T) {
	const value = 100
	src, err := NewMatInt32Int32Int32(5, 5, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer src.Close()
	fill := bgr(t, src)
	for i := range fill {
		fill[i] = value
	}

	for _, tc := range []struct {
		name string
		blur func(*InputArray, *OutputArray) error
	}{
		{"blur", func(in *InputArray, out *OutputArray) error {
			return BlurInputArrayPtrRefOutputArrayPtrRefSizeInt(in, out, SizeInt{Width: 3, Height: 3})
		}},
		{"GaussianBlur", func(in *InputArray, out *OutputArray) error {
			return GaussianBlurInputArrayPtrRefOutputArrayPtrRefSizeIntFloat64(in, out, SizeInt{Width: 3, Height: 3}, 0)
		}},
	} {
		in, err := NewInputArrayMatPtrRef(src)
		if err != nil {
			t.Fatalf("%s: NewInputArrayMatPtrRef: %v", tc.name, err)
		}
		dst, err := NewMat()
		if err != nil {
			t.Fatalf("%s: NewMat: %v", tc.name, err)
		}
		out, err := NewOutputArrayMatPtrRef(dst)
		if err != nil {
			t.Fatalf("%s: NewOutputArrayMatPtrRef: %v", tc.name, err)
		}
		if err := tc.blur(in, out); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for i, got := range bgr(t, dst) {
			if got != value {
				t.Errorf("%s: pixel %d = %d, want %d", tc.name, i, got, value)
			}
		}
		in.Close()
		out.Close()
		dst.Close()
	}
}

// TestCanny is edge detection on a known step: a black image with a white
// square has edges on the square's border and none in its flat interior or in
// the black field. A uniform image gives no edges at all.
func TestCanny(t *testing.T) {
	src, err := NewMatInt32Int32Int32(8, 8, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer src.Close()
	// cv::Mat's constructor allocates without zeroing, so the black field is
	// written explicitly: a matrix left with whatever the allocator returned is
	// a different image, and Canny finds edges in the noise.
	pixels := bgr(t, src)
	for i := range pixels {
		pixels[i] = 0
	}
	for row := 2; row < 6; row++ {
		for col := 2; col < 6; col++ {
			pixels[row*8+col] = 255
		}
	}

	edges := func() []byte {
		in, err := NewInputArrayMatPtrRef(src)
		if err != nil {
			t.Fatalf("NewInputArrayMatPtrRef: %v", err)
		}
		defer in.Close()
		dst, err := NewMat()
		if err != nil {
			t.Fatalf("NewMat: %v", err)
		}
		defer dst.Close()
		out, err := NewOutputArrayMatPtrRef(dst)
		if err != nil {
			t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
		}
		defer out.Close()
		if err := CannyInputArrayPtrRefOutputArrayPtrRefFloat64Float64Int32Bool(in, out, 50, 150, 3, false); err != nil {
			t.Fatalf("Canny: %v", err)
		}
		return append([]byte(nil), bgr(t, dst)...)
	}

	found := edges()
	// The border between the square and the background is where the gradient
	// is: at least the eight pixels of the square's left and right edges are
	// detected, and the flat interior (rows 3-4, cols 3-4) is not.
	if found[3*8+3] != 0 || found[4*8+4] != 0 {
		t.Errorf("Canny marked a flat interior pixel: %v", found)
	}
	edgeCount := 0
	for _, v := range found {
		if v != 0 {
			edgeCount++
		}
	}
	if edgeCount < 8 {
		t.Errorf("Canny found %d edge pixels on a 4x4 square, want at least 8: %v", edgeCount, found)
	}
	// The black corner is far from the square and has no gradient.
	if found[0] != 0 {
		t.Errorf("Canny marked the black corner: %v", found)
	}
}

// TestArithmetic covers add, subtract, multiply and divide on known 8-bit
// matrices, including the saturating conversion OpenCV documents (the result is
// saturated to the element type, so 200+100 is 255).
func TestArithmetic(t *testing.T) {
	makeMat := func(values ...byte) *Mat {
		t.Helper()
		m, err := NewMatInt32Int32Int32(1, int32(len(values)), cv8UC1)
		if err != nil {
			t.Fatalf("NewMatInt32Int32Int32: %v", err)
		}
		t.Cleanup(func() { m.Close() })
		copy(bgr(t, m), values)
		return m
	}
	input := func(m *Mat) *InputArray {
		t.Helper()
		in, err := NewInputArrayMatPtrRef(m)
		if err != nil {
			t.Fatalf("NewInputArrayMatPtrRef: %v", err)
		}
		t.Cleanup(func() { in.Close() })
		return in
	}
	result := func(op func(*InputArray, *InputArray, *OutputArray) error, a, b *Mat) []byte {
		t.Helper()
		dst, err := NewMat()
		if err != nil {
			t.Fatalf("NewMat: %v", err)
		}
		t.Cleanup(func() { dst.Close() })
		out, err := NewOutputArrayMatPtrRef(dst)
		if err != nil {
			t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
		}
		t.Cleanup(func() { out.Close() })
		if err := op(input(a), input(b), out); err != nil {
			t.Fatalf("arithmetic: %v", err)
		}
		return append([]byte(nil), bgr(t, dst)...)
	}

	a := makeMat(10, 200, 50, 255)
	b := makeMat(5, 100, 100, 1)

	for _, tc := range []struct {
		name string
		op   func(*InputArray, *InputArray, *OutputArray) error
		want []byte
	}{
		{"add", AddInputArrayPtrRefInputArrayPtrRefOutputArrayPtrRef, []byte{15, 255, 150, 255}},
		{"subtract", SubtractInputArrayPtrRefInputArrayPtrRefOutputArrayPtrRef, []byte{5, 100, 0, 254}},
		{"multiply", MultiplyInputArrayPtrRefInputArrayPtrRefOutputArrayPtrRef, []byte{50, 255, 255, 255}},
		{"divide", DivideInputArrayPtrRefInputArrayPtrRefOutputArrayPtrRef, []byte{2, 2, 0, 255}},
	} {
		if got := result(tc.op, a, b); string(got) != string(tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestReductions covers norm, mean, sum and minMaxLoc on one known matrix.
// The 3x3 values are 1..9, so:
//
//	norm L2 = sqrt(285), norm L1 = 45
//	mean    = 5 (all four channels: the other three are zero)
//	sum     = (45, 0, 0, 0)
//	min     = 1 at (0, 0), max = 9 at (2, 2)
func TestReductions(t *testing.T) {
	const size = 3
	values := make([]byte, 0, size*size)
	for i := 1; i <= size*size; i++ {
		values = append(values, byte(i))
	}
	m, err := NewMatInt32Int32Int32(size, size, cv8UC1)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer m.Close()
	copy(bgr(t, m), values)

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

	l2, err := Norm(in, int32(NormTypesNORML2), empty)
	if err != nil {
		t.Fatalf("Norm: %v", err)
	}
	if math.Abs(l2-math.Sqrt(285)) > 1e-6 {
		t.Errorf("norm L2 of 1..9 = %v, want sqrt(285) = %v", l2, math.Sqrt(285))
	}
	l1, err := Norm(in, int32(NormTypesNORML1), empty)
	if err != nil {
		t.Fatalf("Norm: %v", err)
	}
	if math.Abs(l1-45) > 1e-9 {
		t.Errorf("norm L1 of 1..9 = %v, want 45", l1)
	}

	mean, err := MeanInputArrayPtrRef(in)
	if err != nil {
		t.Fatalf("Mean: %v", err)
	}
	// Scalar's four channels: the matrix is single-channel, so the mean is one
	// value and the other channels are zero.
	if mean.Val[0] != 5 {
		t.Errorf("mean channel 0 = %v, want 5", mean.Val[0])
	}
	for i := 1; i < 4; i++ {
		if mean.Val[i] != 0 {
			t.Errorf("mean channel %d = %v, want 0", i, mean.Val[i])
		}
	}

	sum, err := Sum(in)
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	if sum.Val[0] != 45 {
		t.Errorf("sum channel 0 = %v, want 45", sum.Val[0])
	}
	if sum.Val[1] != 0 || sum.Val[2] != 0 || sum.Val[3] != 0 {
		t.Errorf("sum of a single-channel matrix = %v, want {45, 0, 0, 0}", sum.Val)
	}

	var minVal, maxVal float64
	minLoc := PointInt{}
	maxLoc := PointInt{}
	if err := MinMaxLocInputArrayPtrRefFloat64PtrFloat64PtrPointIntPtrPointIntPtr(in, &minVal, &maxVal, &minLoc, &maxLoc); err != nil {
		t.Fatalf("MinMaxLoc: %v", err)
	}
	if minVal != 1 || maxVal != 9 {
		t.Errorf("min/max = %v/%v, want 1/9", minVal, maxVal)
	}
	if minLoc.X != 0 || minLoc.Y != 0 {
		t.Errorf("min location = (%d, %d), want (0, 0)", minLoc.X, minLoc.Y)
	}
	if maxLoc.X != 2 || maxLoc.Y != 2 {
		t.Errorf("max location = (%d, %d), want (2, 2)", maxLoc.X, maxLoc.Y)
	}
}

// TestContourAreaAndBoundingRect builds a contour the C++ API's own shape: an
// Nx1 two-channel integer matrix of points (CV_32SC2), which is what
// findContours produces and what contourArea and boundingRect consume.
//
// The square (0,0) (4,0) (4,4) (0,4) has area 16 and bounding rectangle
// (0, 0, 5, 5): boundingRect is inclusive of the last point.
func TestContourAreaAndBoundingRect(t *testing.T) {
	const cv32SC2 = int32(4 + (2-1)<<3) // CV_MAKETYPE(CV_32S, 2)
	points := [][2]int32{{0, 0}, {4, 0}, {4, 4}, {0, 4}}
	m, err := NewMatInt32Int32Int32(int32(len(points)), 1, cv32SC2)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32: %v", err)
	}
	defer m.Close()
	raw := unsafe.Slice((*int32)(unsafe.Pointer(m.Data())), len(points)*2)
	for i, p := range points {
		raw[i*2], raw[i*2+1] = p[0], p[1]
	}

	in, err := NewInputArrayMatPtrRef(m)
	if err != nil {
		t.Fatalf("NewInputArrayMatPtrRef: %v", err)
	}
	defer in.Close()

	area, err := ContourAreaInputArrayPtrRef(in)
	if err != nil {
		t.Fatalf("ContourArea: %v", err)
	}
	if area != 16 {
		t.Errorf("contourArea = %v, want 16", area)
	}

	rect, err := BoundingRect(in)
	if err != nil {
		t.Fatalf("BoundingRect: %v", err)
	}
	if rect.X != 0 || rect.Y != 0 || rect.Width != 5 || rect.Height != 5 {
		t.Errorf("boundingRect = (%d, %d, %d, %d), want (0, 0, 5, 5)", rect.X, rect.Y, rect.Width, rect.Height)
	}
}

// min is the smaller of two ints, for the slice bound in a failure message.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestMatFromRawData wraps bytes the Go side owns in a cv::Mat: the
// constructor takes the buffer's address and does not copy, so OpenCV reads and
// writes the caller's memory. It is the same contract the C++ declaration has,
// and the caller keeps the buffer alive for as long as the matrix is used.
func TestMatFromRawData(t *testing.T) {
	buffer := []byte{1, 2, 3, 4, 5, 6}
	m, err := NewMatInt32Int32Int32UnsafePointerUint64(1, 2, cv8UC3, unsafe.Pointer(&buffer[0]), 0)
	if err != nil {
		t.Fatalf("NewMatInt32Int32Int32UnsafePointerUint64: %v", err)
	}
	defer m.Close()
	if got, want := bgr(t, m), buffer; string(got) != string(want) {
		t.Errorf("matrix over the caller's buffer = %v, want %v", got, want)
	}
	// The buffer is shared, not copied: a write through the matrix is a write
	// into the Go slice.
	bgr(t, m)[0] = 42
	if buffer[0] != 42 {
		t.Errorf("the Go buffer after writing the matrix = %d, want 42 (no copy)", buffer[0])
	}
	runtime.KeepAlive(buffer)
}

// TestGeometryRecords covers the three mirrored records and one method on each:
// cv::Point_<int>, cv::Size_<int> and cv::Rect_<int> are Go structs whose fields
// are the C++ fields, and the methods the generator bound are C++ inline
// functions over those fields.
func TestGeometryRecords(t *testing.T) {
	a := PointInt{X: 3, Y: 4}
	b := PointInt{X: 1, Y: 2}

	// cv::Point_::dot is x*x' + y*y' (types.hpp), and cross is the 2D cross
	// product x*y' - y*x'.
	dot, err := a.Dot(&b)
	if err != nil {
		t.Fatalf("Dot: %v", err)
	}
	if dot != 3*1+4*2 {
		t.Errorf("dot = %d, want %d", dot, 3*1+4*2)
	}
	cross, err := a.Cross(&b)
	if err != nil {
		t.Fatalf("Cross: %v", err)
	}
	if cross != float64(3*2-4*1) {
		t.Errorf("cross = %v, want %v", cross, float64(3*2-4*1))
	}

	// cv::Size_::area is width*height, and empty asks whether either is zero.
	size := SizeInt{Width: 4, Height: 5}
	if area, err := size.Area(); err != nil || area != 20 {
		t.Errorf("Size area = %d, %v, want 20, nil", area, err)
	}
	if empty, err := size.Empty(); err != nil || empty {
		t.Errorf("Size empty = %v, %v, want false, nil", empty, err)
	}

	// cv::Rect_::contains reports whether a point is inside the half-open
	// rectangle the four fields describe.
	rect := RectInt{X: 1, Y: 1, Width: 3, Height: 3}
	if inside, err := rect.Contains(&PointInt{X: 2, Y: 2}); err != nil || !inside {
		t.Errorf("rect contains (2, 2) = %v, %v, want true, nil", inside, err)
	}
	if inside, err := rect.Contains(&PointInt{X: 5, Y: 5}); err != nil || inside {
		t.Errorf("rect contains (5, 5) = %v, %v, want false, nil", inside, err)
	}
}
