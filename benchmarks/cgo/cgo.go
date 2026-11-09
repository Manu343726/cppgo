// Package benchcgo is the cgo arm of the comparison: the same libbench.so the
// cppgo arm loads, reached through the hand-written extern "C" shim in
// shim.cpp.
//
// This package needs CGO_ENABLED=1. It is the only package in the module that
// does, and `make bench` runs it under its own environment; everything else
// (the cppgo arm and the plain-Go baseline) builds and runs with CGO_ENABLED=0.
//
// The wrapper functions return Go values and Go errors, never C types, so the
// benchmarks in bench_test.go measure the same API shape a caller of the cppgo
// package gets.
package benchcgo

/*
#cgo CXXFLAGS: -std=c++17 -O2 -I${SRCDIR}/../include
#cgo LDFLAGS: -L${SRCDIR}/../build -lbench -lstdc++ -Wl,-rpath,${SRCDIR}/../build
#include "bench_c.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Point is the Go copy of C++'s bench::Point: 16 bytes, same layout, and the
// call copies it through the C ABI by value.
type Point struct {
	X float64
	Y float64
}

// Sample is the 24-byte record: a result above the register class, so the
// shim writes it through the out-parameter the Go caller owns.
type Sample struct {
	A float64
	B float64
	C float64
}

// lastError is the Go-visible failure of a wrapper: the message the shim
// recorded, named by the declaration that failed. Reading it costs one
// C.GoString copy, which is what any cgo binding pays for a C string.
func lastError(symbol string) error {
	msg := C.GoString(C.bench_last_error())
	if msg == "" {
		return fmt.Errorf("bench: %s: the call failed without a message", symbol)
	}
	return fmt.Errorf("bench: %s: %s", symbol, msg)
}

// Add calls bench::add through its direct, noexcept entry point.
func Add(a, b int64) int64 {
	return int64(C.bench_add(C.int64_t(a), C.int64_t(b)))
}

// AddThunk calls bench::add_thunk, the same arithmetic declared without
// noexcept, and pays the status/out-parameter shape cgo wrappers need.
func AddThunk(a, b int64) (int64, error) {
	var out C.int64_t
	if status := C.bench_add_thunk(C.int64_t(a), C.int64_t(b), &out); status != 0 {
		return 0, lastError("bench::add_thunk")
	}
	return int64(out), nil
}

// Midpoint copies both arguments into the C ABI and the result back out.
func Midpoint(a, b Point) (Point, error) {
	ca := C.struct_bench_point{x: C.double(a.X), y: C.double(a.Y)}
	cb := C.struct_bench_point{x: C.double(b.X), y: C.double(b.Y)}
	var out C.struct_bench_point
	if status := C.bench_midpoint(ca, cb, &out); status != 0 {
		return Point{}, lastError("bench::midpoint")
	}
	return Point{X: float64(out.x), Y: float64(out.y)}, nil
}

// Blend is the noexcept, larger-than-register result: no error channel, and
// the result is written through the caller's storage.
func Blend(a, b Sample, t float64) Sample {
	ca := C.struct_bench_sample{a: C.double(a.A), b: C.double(a.B), c: C.double(a.C)}
	cb := C.struct_bench_sample{a: C.double(b.A), b: C.double(b.B), c: C.double(b.C)}
	var out C.struct_bench_sample
	C.bench_blend(ca, cb, C.double(t), &out)
	return Sample{A: float64(out.a), B: float64(out.b), C: float64(out.c)}
}

// Describe passes the string's bytes and length, not a C copy: the C++ side
// builds its own std::string, exactly as cppgo's generated thunk does, so the
// only difference between the arms is the crossing.
func Describe(prefix string, value float64) (string, error) {
	var data unsafe.Pointer
	var size C.size_t
	status := C.bench_describe(
		(*C.char)(unsafe.Pointer(unsafe.StringData(prefix))), C.size_t(len(prefix)),
		C.double(value), &data, &size)
	if status != 0 {
		return "", lastError("bench::describe")
	}
	result := C.GoStringN((*C.char)(data), C.int(size))
	C.bench_free(data)
	return result, nil
}

// Doubled passes the borrowed elements and copies the result out.
func Doubled(values []float64) ([]float64, error) {
	var data unsafe.Pointer
	var size C.size_t
	var ptr *C.double
	if len(values) != 0 {
		ptr = (*C.double)(unsafe.Pointer(&values[0]))
	}
	status := C.bench_doubled(ptr, C.size_t(len(values)), &data, &size)
	if status != 0 {
		return nil, lastError("bench::doubled")
	}
	var result []float64
	if size != 0 {
		result = append(result, unsafe.Slice((*float64)(data), int(size))...)
	}
	C.bench_free(data)
	return result, nil
}

// Ratio is the throwing path: the shim catches std::domain_error, and the Go
// side builds an error from the message it recorded.
func Ratio(a, b int32) (int32, error) {
	var out C.int32_t
	if status := C.bench_ratio(C.int32_t(a), C.int32_t(b), &out); status != 0 {
		return 0, lastError("bench::ratio")
	}
	return int32(out), nil
}

// Parse is the status convention: a non-zero integer is the failure, and the
// Go signature returns only the error.
func Parse(text string) error {
	status := C.bench_parse(
		(*C.char)(unsafe.Pointer(unsafe.StringData(text))), C.size_t(len(text)))
	if status != 0 {
		return lastError("bench::parse")
	}
	return nil
}

// Fold is the C++ -> Go round trip: the shim's bench_fold builds a
// std::function from the exported Go function and calls bench::fold.
func Fold(fn func(int32, int32) int32, a, b int32) (int32, error) {
	foldFn = fn // the exported callback reads this; see export.go
	var out C.int32_t
	if status := C.bench_fold(C.int32_t(a), C.int32_t(b), &out); status != 0 {
		return 0, lastError("bench::fold")
	}
	return int32(out), nil
}

// Counter owns a C++-allocated bench::Counter. Close destroys it through the
// destructor, exactly like the generated package's Close.
type Counter struct {
	ptr unsafe.Pointer
}

// NewCounter constructs the object on the C++ heap.
func NewCounter(start int64) (*Counter, error) {
	ptr := C.bench_counter_new(C.int64_t(start))
	if ptr == nil {
		return nil, lastError("bench::Counter::Counter")
	}
	return &Counter{ptr: ptr}, nil
}

// Close destroys the object and returns cppgo.ErrClosed's shape: nil on the
// first call, an error afterwards.
func (c *Counter) Close() error {
	if c == nil || c.ptr == nil {
		return fmt.Errorf("bench: bench::Counter::~Counter: the object is already closed")
	}
	C.bench_counter_free(c.ptr)
	c.ptr = nil
	return nil
}

// Get reads the value: the direct call, noexcept in C++.
func (c *Counter) Get() int64 { return int64(C.bench_counter_get(c.ptr)) }

// Bump adds to the value in place.
func (c *Counter) Bump(by int64) { C.bench_counter_bump(c.ptr, C.int64_t(by)) }
