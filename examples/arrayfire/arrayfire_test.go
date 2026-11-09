//go:build cppgo_arrayfire

package arrayfire

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// newDim4 is af::dim4(first, second, third, fourth), the shape every array
// constructor and af::range take.
func newDim4(t *testing.T, first, second, third, fourth int64) *Dim4 {
	t.Helper()
	dims, err := NewDim4(first, second, third, fourth)
	if err != nil {
		t.Fatalf("NewDim4(%d, %d, %d, %d): %v", first, second, third, fourth, err)
	}
	t.Cleanup(func() {
		if err := dims.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return dims
}

// norm1 is af::norm with AF_NORM_VECTOR_1: the sum of the absolute values of
// every element. It is how this test reads an array's contents back: array's
// scalar<T>() and host<T>() are member function templates the generator does
// not carry (README.md records the refusal), so the library's own reduction is
// the honest way to observe a value from Go.
func norm1(t *testing.T, a *Array) float64 {
	t.Helper()
	got, err := Norm(a, AfNormTypeAFNORMVECTOR1, 0, 0)
	if err != nil {
		t.Fatalf("Norm(AF_NORM_VECTOR_1): %v", err)
	}
	return got
}

// The binding loads two objects: libcppgo_arrayfire.so, the shim cppgo compiled
// from the generated sources, and libaf.so, which that shim names. The test
// names both, because the loader does not search the current directory.
//
// The library is the system's, so its path is a variable: CPPGO_ARRAYFIRE_LIB is
// how the Makefile passes the one it found, and the loader needs the directory
// that holds it on LD_LIBRARY_PATH when a distribution keeps it outside the
// default search path.
func init() {
	path := os.Getenv("CPPGO_ARRAYFIRE_LIB")
	if path == "" {
		path = "/usr/local/lib/libaf.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}

// shimPath is libcppgo_arrayfire.so: the shared object cppgo compiled from the
// generated shim and linked against libaf.so.
func shimPath() string {
	path := os.Getenv("CPPGO_ARRAYFIRE_SHIM")
	if path == "" {
		path = "libcppgo_arrayfire.so"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

// TestMain opens the generated binding. A machine without ArrayFire skips the
// whole file with the reason, which is what the build tag does on a machine that
// cannot compile it and what this does on one that compiles but cannot load.
func TestMain(m *testing.M) {
	if _, err := os.Stat(CppgoLibraryPath); err != nil {
		fmt.Fprintf(os.Stderr, "arrayfire: skipping, libaf.so is missing: %v\n"+
			"install libarrayfire-dev, or set CPPGO_ARRAYFIRE_LIB\n", err)
		os.Exit(0)
	}
	if err := Open(shimPath()); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestDtypeConstants checks the element-type enum against the values its
// declaration gives them. af/defines.h declares it without explicit values, in
// this order, and each comment states the width the enumerator names:
//
//   - f32, c32, f64, c64, b8, s32, u32, u8, s64, u64 (always),
//     then s16, u16 (API 3.2), f16 (3.7) and s8 (3.10).
func TestDtypeConstants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  AfDtype
		want AfDtype
	}{
		{"AfDtypeF32", AfDtypeF32, 0},
		{"AfDtypeC32", AfDtypeC32, 1},
		{"AfDtypeF64", AfDtypeF64, 2},
		{"AfDtypeC64", AfDtypeC64, 3},
		{"AfDtypeB8", AfDtypeB8, 4},
		{"AfDtypeS32", AfDtypeS32, 5},
		{"AfDtypeU32", AfDtypeU32, 6},
		{"AfDtypeU8", AfDtypeU8, 7},
		{"AfDtypeS64", AfDtypeS64, 8},
		{"AfDtypeU64", AfDtypeU64, 9},
		{"AfDtypeS16", AfDtypeS16, 10},
		{"AfDtypeU16", AfDtypeU16, 11},
		{"AfDtypeF16", AfDtypeF16, 12},
		{"AfDtypeS8", AfDtypeS8, 13},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestSourceConstants checks the data-source enum: afDevice is declared first
// and afHost second, so a pointer is read from the device unless the caller says
// otherwise.
func TestSourceConstants(t *testing.T) {
	if AfSourceAfDevice != 0 {
		t.Errorf("AfSourceAfDevice = %d, want 0", AfSourceAfDevice)
	}
	if AfSourceAfHost != 1 {
		t.Errorf("AfSourceAfHost = %d, want 1", AfSourceAfHost)
	}
}

// TestSizeOf is the one function that ties the enum to a value: getSizeOf
// reports the width of the type an enumerator names, and the widths are the ones
// the enumerators' own names state (f32 is 32 bits, c64 is 64 bits per
// component and therefore 16 bytes, b8 and s8 are 8 bits, and so on).
func TestSizeOf(t *testing.T) {
	for _, tc := range []struct {
		dtype AfDtype
		want  uint64
	}{
		{AfDtypeF32, 4},
		{AfDtypeC32, 8},
		{AfDtypeF64, 8},
		{AfDtypeC64, 16},
		{AfDtypeB8, 1},
		{AfDtypeS32, 4},
		{AfDtypeU32, 4},
		{AfDtypeU8, 1},
		{AfDtypeS64, 8},
		{AfDtypeU64, 8},
		{AfDtypeS16, 2},
		{AfDtypeU16, 2},
		{AfDtypeF16, 2},
		{AfDtypeS8, 1},
	} {
		got, err := GetSizeOf(tc.dtype)
		if err != nil {
			t.Errorf("GetSizeOf(%d): %v", tc.dtype, err)
			continue
		}
		if got != tc.want {
			t.Errorf("GetSizeOf(%d) = %d, want %d", tc.dtype, got, tc.want)
		}
	}
}

// TestDevice reports what the loaded backend says about itself: at least one
// device exists, the current one is index 0 until something sets another, the
// memory manager hands out chunks of a non-zero size, and the backend answers
// the precision question.
func TestDevice(t *testing.T) {
	count, err := GetDeviceCount()
	if err != nil {
		t.Fatalf("GetDeviceCount: %v", err)
	}
	if count < 1 {
		t.Fatalf("GetDeviceCount() = %d, want at least 1", count)
	}

	current, err := GetDevice()
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if current != 0 {
		t.Errorf("GetDevice() = %d, want 0", current)
	}

	step, err := GetMemStepSize()
	if err != nil {
		t.Fatalf("GetMemStepSize: %v", err)
	}
	if step == 0 {
		t.Error("GetMemStepSize() = 0, want a non-zero chunk size")
	}

	// The CPU backend computes in double precision, so the answer is true. The
	// call is still asserted against the value it returns rather than assumed:
	// a backend that cannot do it is a fact about the device, not a test failure.
	double, err := IsDoubleAvailable(0)
	if err != nil {
		t.Fatalf("IsDoubleAvailable(0): %v", err)
	}
	if !double {
		t.Error("IsDoubleAvailable(0) = false, want true for the CPU backend")
	}

	// isHalfAvailable is the one answer that legitimately differs between
	// backends: the call must succeed and report a bool, whichever it is.
	if _, err := IsHalfAvailable(0); err != nil {
		t.Errorf("IsHalfAvailable(0): %v", err)
	}
}

// TestArrayConstruction covers af::array's carryable constructors and the
// accessors that report its shape and element type. The dimensions are read
// one at a time through dims(unsigned), because af::dim4's fields are not part
// of the generated API (README.md records why).
func TestArrayConstruction(t *testing.T) {
	a, err := NewArrayInt64Int64AfDtype(2, 3, AfDtypeF64)
	if err != nil {
		t.Fatalf("NewArrayInt64Int64AfDtype(2, 3, f64): %v", err)
	}
	defer a.Close()

	if got, err := a.Elements(); err != nil || got != 6 {
		t.Errorf("Elements() = %d, %v, want 6, nil", got, err)
	}
	if got, err := a.Type(); err != nil || got != AfDtypeF64 {
		t.Errorf("Type() = %d, %v, want %d (f64), nil", got, err, AfDtypeF64)
	}
	if got, err := a.DimsUint32(0); err != nil || got != 2 {
		t.Errorf("dims(0) = %d, %v, want 2, nil", got, err)
	}
	if got, err := a.DimsUint32(1); err != nil || got != 3 {
		t.Errorf("dims(1) = %d, %v, want 3, nil", got, err)
	}
	if got, err := a.Isempty(); err != nil || got {
		t.Errorf("Isempty() of a 2x3 array = %v, %v, want false, nil", got, err)
	}

	// The default constructor makes an empty array: no elements and no buffer.
	empty, err := NewArray()
	if err != nil {
		t.Fatalf("NewArray: %v", err)
	}
	defer empty.Close()
	if got, err := empty.Elements(); err != nil || got != 0 {
		t.Errorf("Elements() of the default af::array = %d, %v, want 0, nil", got, err)
	}
	if got, err := empty.Isempty(); err != nil || !got {
		t.Errorf("Isempty() of the default af::array = %v, %v, want true, nil", got, err)
	}
}

// TestArrayLifetime is the ownership rule every generated value shares: Close
// releases the C++ object and a second Close reports it, and a method on a
// closed value returns ErrClosed instead of calling into freed memory.
func TestArrayLifetime(t *testing.T) {
	a, err := NewArrayInt64AfDtype(4, AfDtypeF32)
	if err != nil {
		t.Fatalf("NewArrayInt64AfDtype: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := a.Close(); err == nil {
		t.Error("second Close = nil, want an error")
	}
	if _, err := a.Elements(); err == nil {
		t.Error("Elements() on a closed array = nil error, want ErrClosed")
	}
}

// TestRangeAndIdentity covers the two value-producing constructors that carry:
// af::range fills [0, n-1] along the dimension it is given, and af::identity
// puts ones on the diagonal. Both are exact, which is what lets the reductions
// below assert a value rather than a shape.
func TestRangeAndIdentity(t *testing.T) {
	// A 4x1 column: range along dimension 0 is 0, 1, 2, 3 (af/data.h's
	// "generate an array with [0, n-1] values along the seq_dim dimension").
	dims := newDim4(t, 4, 1, 1, 1)
	v, err := Range(dims, 0, AfDtypeF64)
	if err != nil {
		t.Fatalf("Range(4x1): %v", err)
	}
	defer v.Close()
	if got, err := v.Elements(); err != nil || got != 4 {
		t.Errorf("Elements() = %d, %v, want 4, nil", got, err)
	}
	if got := norm1(t, v); got != 6 {
		t.Errorf("sum(|[0 1 2 3]|) = %v, want 6", got)
	}

	// The 2x2 identity has two ones, whatever the layout.
	id, err := Identity(2, 2, AfDtypeF64)
	if err != nil {
		t.Fatalf("Identity(2, 2): %v", err)
	}
	defer id.Close()
	if got, err := id.Elements(); err != nil || got != 4 {
		t.Errorf("Elements() of Identity(2,2) = %d, %v, want 4, nil", got, err)
	}
	if got := norm1(t, id); got != 2 {
		t.Errorf("sum(|Identity(2,2)|) = %v, want 2", got)
	}
}

// TestReductions covers af::sum, af::max and af::min over known data. The
// matrix is v * transpose(v), so its element (i, j) is i*j and every reduction
// has an exact integer answer.
func TestReductions(t *testing.T) {
	dims := newDim4(t, 4, 1, 1, 1)
	v, err := Range(dims, 0, AfDtypeF64)
	if err != nil {
		t.Fatalf("Range(4x1): %v", err)
	}
	defer v.Close()
	vt, err := Transpose(v, false)
	if err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	defer vt.Close()
	m, err := Matmul(v, vt, AfMatPropAFMATNONE, AfMatPropAFMATNONE)
	if err != nil {
		t.Fatalf("Matmul: %v", err)
	}
	defer m.Close()

	// Along dimension 0 the column sums are j*(0+1+2+3) = 6j: 0, 6, 12, 18.
	sum0, err := SumArrayPtrRefInt32(m, 0)
	if err != nil {
		t.Fatalf("Sum(m, 0): %v", err)
	}
	defer sum0.Close()
	if got, err := sum0.Elements(); err != nil || got != 4 {
		t.Errorf("Elements() of Sum(m, 0) = %d, %v, want 4, nil", got, err)
	}
	if got := norm1(t, sum0); got != 36 {
		t.Errorf("sum(|Sum(m, 0)|) = %v, want 36 (0+6+12+18)", got)
	}

	// The nanval overload reduces the same way when no NaN is present.
	sumNaN, err := SumArrayPtrRefInt32Float64(m, 0, 0)
	if err != nil {
		t.Fatalf("Sum(m, 0, 0): %v", err)
	}
	defer sumNaN.Close()
	if got := norm1(t, sumNaN); got != 36 {
		t.Errorf("sum(|Sum(m, 0, nanval)|) = %v, want 36", got)
	}

	// The row sums are i*6, the same multiset.
	sum1, err := SumArrayPtrRefInt32(m, 1)
	if err != nil {
		t.Fatalf("Sum(m, 1): %v", err)
	}
	defer sum1.Close()
	if got := norm1(t, sum1); got != 36 {
		t.Errorf("sum(|Sum(m, 1)|) = %v, want 36", got)
	}

	// The column maxima are j*3: 0, 3, 6, 9.
	max, err := Max(m, 0)
	if err != nil {
		t.Fatalf("Max(m, 0): %v", err)
	}
	defer max.Close()
	if got := norm1(t, max); got != 18 {
		t.Errorf("sum(|Max(m, 0)|) = %v, want 18 (0+3+6+9)", got)
	}

	// The column minima are all 0, because i*j is 0 in the first row.
	min, err := Min(m, 0)
	if err != nil {
		t.Fatalf("Min(m, 0): %v", err)
	}
	defer min.Close()
	if got := norm1(t, min); got != 0 {
		t.Errorf("sum(|Min(m, 0)|) = %v, want 0", got)
	}

	// The vector norms are the ones their names state: the 1-norm is 6 and the
	// 2-norm is sqrt(0+1+4+9) = sqrt(14) for the 4x1 range.
	l2, err := Norm(v, AfNormTypeAFNORMVECTOR2, 0, 0)
	if err != nil {
		t.Fatalf("Norm(AF_NORM_VECTOR_2): %v", err)
	}
	if math.Abs(l2-math.Sqrt(14)) > 1e-12 {
		t.Errorf("Norm(AF_NORM_VECTOR_2) of [0 1 2 3] = %v, want sqrt(14) = %v", l2, math.Sqrt(14))
	}
	l1, err := Norm(v, AfNormTypeAFNORMVECTOR1, 0, 0)
	if err != nil {
		t.Fatalf("Norm(AF_NORM_VECTOR_1): %v", err)
	}
	if l1 != 6 {
		t.Errorf("Norm(AF_NORM_VECTOR_1) of [0 1 2 3] = %v, want 6", l1)
	}
}

// TestMatmul multiplies the 4x1 range by its transpose: (i, j) is i*j and the
// sum of every element is (0+1+2+3)^2 = 36. The identity is the second
// operand: identity times anything is that thing.
func TestMatmul(t *testing.T) {
	dims := newDim4(t, 4, 1, 1, 1)
	v, err := Range(dims, 0, AfDtypeF64)
	if err != nil {
		t.Fatalf("Range(4x1): %v", err)
	}
	defer v.Close()
	vt, err := Transpose(v, false)
	if err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	defer vt.Close()

	m, err := Matmul(v, vt, AfMatPropAFMATNONE, AfMatPropAFMATNONE)
	if err != nil {
		t.Fatalf("Matmul: %v", err)
	}
	defer m.Close()
	if got, err := m.Elements(); err != nil || got != 16 {
		t.Errorf("Elements() of the 4x4 product = %d, %v, want 16, nil", got, err)
	}
	if got, err := m.DimsUint32(0); err != nil || got != 4 {
		t.Errorf("dims(0) of the product = %d, %v, want 4, nil", got, err)
	}
	if got, err := m.DimsUint32(1); err != nil || got != 4 {
		t.Errorf("dims(1) of the product = %d, %v, want 4, nil", got, err)
	}
	if got := norm1(t, m); got != 36 {
		t.Errorf("sum(|v * transpose(v)|) = %v, want 36 ((0+1+2+3)^2)", got)
	}

	// identity(4) * v is v, whose elements are 0, 1, 2, 3.
	id, err := Identity(4, 4, AfDtypeF64)
	if err != nil {
		t.Fatalf("Identity(4,4): %v", err)
	}
	defer id.Close()
	same, err := Matmul(id, v, AfMatPropAFMATNONE, AfMatPropAFMATNONE)
	if err != nil {
		t.Fatalf("Matmul(identity, v): %v", err)
	}
	defer same.Close()
	if got := norm1(t, same); got != 6 {
		t.Errorf("sum(|identity * v|) = %v, want 6", got)
	}

	// The transpose option is the same product with the left operand
	// transposed: the 1x4 row times the 4x1 column is a 1x1 scalar, 0+1+4+9.
	dot, err := Matmul(vt, v, AfMatPropAFMATNONE, AfMatPropAFMATNONE)
	if err != nil {
		t.Fatalf("Matmul(vt, v): %v", err)
	}
	defer dot.Close()
	if got, err := dot.Elements(); err != nil || got != 1 {
		t.Errorf("Elements() of the inner product = %d, %v, want 1, nil", got, err)
	}
	if got := norm1(t, dot); got != 14 {
		t.Errorf("sum(|vt * v|) = %v, want 14 (0+1+4+9)", got)
	}
}

// TestDtypePath is the dtype half of the issue's matrix: an array reports the
// type it was built with, and as() converts it.
func TestDtypePath(t *testing.T) {
	f64, err := NewArrayInt64AfDtype(4, AfDtypeF64)
	if err != nil {
		t.Fatalf("NewArrayInt64AfDtype(4, f64): %v", err)
	}
	defer f64.Close()
	if got, err := f64.Type(); err != nil || got != AfDtypeF64 {
		t.Errorf("Type() = %d, %v, want %d (f64), nil", got, err, AfDtypeF64)
	}

	f32, err := f64.As(AfDtypeF32)
	if err != nil {
		t.Fatalf("As(f32): %v", err)
	}
	defer f32.Close()
	if got, err := f32.Type(); err != nil || got != AfDtypeF32 {
		t.Errorf("Type() after As(f32) = %d, %v, want %d (f32), nil", got, err, AfDtypeF32)
	}
	if got, err := f32.Elements(); err != nil || got != 4 {
		t.Errorf("Elements() after As(f32) = %d, %v, want 4, nil", got, err)
	}

	// identity takes the element type directly, so the dtype reaches the
	// constructor too.
	id, err := Identity(2, 2, AfDtypeF32)
	if err != nil {
		t.Fatalf("Identity(2, 2, f32): %v", err)
	}
	defer id.Close()
	if got, err := id.Type(); err != nil || got != AfDtypeF32 {
		t.Errorf("Type() of the f32 identity = %d, %v, want %d (f32), nil", got, err, AfDtypeF32)
	}
}

// TestSeq is the seq half: af::seq is the index range ArrayFire's array
// subscript takes, and its fields are the begin, end and step the constructor
// was given (af/seq.h documents each constructor's [begin, end, step]).
func TestSeq(t *testing.T) {
	s, err := NewSeqFloat64Float64Float64(10, 20, 2)
	if err != nil {
		t.Fatalf("NewSeqFloat64Float64Float64(10, 20, 2): %v", err)
	}
	defer s.Close()
	// 10, 12, 14, 16, 18, 20: six values, inclusive of both ends.
	if got := s.Size(); got != 6 {
		t.Errorf("Size() of seq(10, 20, 2) = %d, want 6", got)
	}
	bounds := s.S()
	if bounds.Begin != 10 || bounds.End != 20 || bounds.Step != 2 {
		t.Errorf("S() of seq(10, 20, 2) = {%v, %v, %v}, want {10, 20, 2}",
			bounds.Begin, bounds.End, bounds.Step)
	}

	// seq(length) is [0, length-1] with step 1; the two-argument form defaults
	// the step to 1.
	length, err := NewSeqFloat64(4)
	if err != nil {
		t.Fatalf("NewSeqFloat64(4): %v", err)
	}
	defer length.Close()
	if got := length.Size(); got != 4 {
		t.Errorf("Size() of seq(4) = %d, want 4", got)
	}
	if got := length.S(); got.Begin != 0 || got.End != 3 || got.Step != 1 {
		t.Errorf("S() of seq(4) = {%v, %v, %v}, want {0, 3, 1}", got.Begin, got.End, got.Step)
	}

	step1, err := NewSeqFloat64Float64(10, 20)
	if err != nil {
		t.Fatalf("NewSeqFloat64Float64(10, 20): %v", err)
	}
	defer step1.Close()
	if got := step1.Size(); got != 11 {
		t.Errorf("Size() of seq(10, 20) = %d, want 11", got)
	}
}

// TestLibraryError is the error path: an invalid operation raises af::exception
// inside the library and the thunk hands it to Go as an error.
func TestLibraryError(t *testing.T) {
	// matmul with a 4x1 left operand and a 4x1 right operand: the inner
	// dimensions (1 and 4) disagree.
	dims := newDim4(t, 4, 1, 1, 1)
	v, err := Range(dims, 0, AfDtypeF64)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	defer v.Close()
	if _, err := Matmul(v, v, AfMatPropAFMATNONE, AfMatPropAFMATNONE); err == nil {
		t.Error("Matmul(4x1, 4x1) = nil error, want the dimension error")
	}

	// A reduction along a dimension the array does not have is the second:
	// the 4x1 column has no dimension 5.
	if _, err := SumArrayPtrRefInt32(v, 5); err == nil {
		t.Error("Sum(v, 5) = nil error, want the out-of-range dimension error")
	}
}
