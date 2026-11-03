//go:build cgo && linux && amd64

package cgotest

/*
#include <stdint.h>

// The same two functions the C++ fixture exports, written in C and compiled
// into this package, so cgo has something to call with an identical body.

static int64_t cgotest_add(int64_t a, int64_t b) { return a + b; }

static int64_t cgotest_add6(int64_t a, int64_t b, int64_t c, int64_t d,
                            int64_t e, int64_t f) {
	return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f;
}
*/
import "C"

// CgoAdd adds two integers through the cgo call path.
func CgoAdd(a, b int64) int64 {
	return int64(C.cgotest_add(C.int64_t(a), C.int64_t(b)))
}

// CgoAdd6 combines six integers through the cgo call path, with the same
// weights as the fixture's cppgo_fixture_add6.
func CgoAdd6(a, b, c, d, e, f int64) int64 {
	return int64(C.cgotest_add6(
		C.int64_t(a), C.int64_t(b), C.int64_t(c),
		C.int64_t(d), C.int64_t(e), C.int64_t(f),
	))
}
