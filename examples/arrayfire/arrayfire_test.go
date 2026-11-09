//go:build cppgo_arrayfire

package arrayfire

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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
