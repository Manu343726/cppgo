// Package benchdata is the plain-Go side of the comparison: one reference
// implementation per piece of work in benchmarks/include/bench/bench.hpp, the
// fixture builders all three arms share, and the sizes the benchmarks run at.
//
// It is the single definition of "the same work". Both FFI arms are tested
// against these functions, and the gobase package benchmarks them directly, so
// a benchmark cannot accidentally compare two different computations.
//
// The //go:noinline directives are load-bearing. A direct call baseline that
// the compiler inlines measures the arithmetic and not the call, and the point
// of the go_direct arm is precisely the cost of a real CALL the compiler could
// not remove.
package benchdata

import (
	"fmt"
	"strconv"
)

const (
	// SmallN and LargeN are the two slice sizes: at SmallN the allocation and
	// the crossing dominate, at LargeN the loop does.
	SmallN = 8
	LargeN = 1 << 10

	// Prefix and Value are the describe fixture: a non-empty string_view and a
	// double, so both a string and an SSE argument cross.
	Prefix = "bench"
	Value  = 1.5
)

// Point mirrors bench::Point: 16 bytes, register class.
type Point struct {
	X float64
	Y float64
}

// Sample mirrors bench::Sample: 24 bytes, larger than the register class.
type Sample struct {
	A float64
	B float64
	C float64
}

// Floats builds the deterministic input the slice benchmarks share.
func Floats(n int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = float64(i%17) / 7.0
	}
	return xs
}

// Points is the midpoint fixture.
func Points() (Point, Point) {
	return Point{X: 1.25, Y: -2.5}, Point{X: 4.75, Y: 6.5}
}

// Samples is the blend fixture.
func Samples() (Sample, Sample) {
	return Sample{A: 1, B: 2, C: 3}, Sample{A: 5, B: 8, C: 13}
}

// Add is bench::add: the crossing floor, with nothing to compute.
//
//go:noinline
func Add(a, b int64) int64 { return a + b }

// AddFn is the same function behind a func value, so the call is indirect
// exactly as a closure or an interface method would make it.
var AddFn = func(a, b int64) int64 { return a + b }

// AddThunk is bench::add_thunk: identical arithmetic, no error channel to
// justify the thunk, so the Go side returns one value too.
//
//go:noinline
func AddThunk(a, b int64) int64 { return a + b }

// Midpoint is bench::midpoint.
//
//go:noinline
func Midpoint(a, b Point) Point {
	return Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
}

// Blend is bench::blend.
//
//go:noinline
func Blend(a, b Sample, t float64) Sample {
	return Sample{
		A: a.A + (b.A-a.A)*t,
		B: a.B + (b.B-a.B)*t,
		C: a.C + (b.C-a.C)*t,
	}
}

// Describe is bench::describe. std::to_string(double) prints six decimals,
// which is what strconv's 'f' format with precision 6 produces, so the two
// sides return the same string and the tests compare them byte for byte.
//
//go:noinline
func Describe(prefix string, value float64) string {
	return prefix + ": " + strconv.FormatFloat(value, 'f', 6, 64)
}

// Doubled is bench::doubled: one new slice, every element doubled.
//
//go:noinline
func Doubled(values []float64) []float64 {
	result := make([]float64, len(values))
	for i, value := range values {
		result[i] = value * 2
	}
	return result
}

// Ratio is bench::ratio: the throwing path becomes an error in Go.
//
//go:noinline
func Ratio(a, b int32) (int32, error) {
	if b == 0 {
		return 0, fmt.Errorf("bench: division by zero")
	}
	return a / b, nil
}

// Parse is bench::parse under the status convention: the integer disappears
// and only the error is left. The messages are the ones the cgo arm builds
// from bench_last_error, so the error arms of the three variants construct
// comparable values.
//
//go:noinline
func Parse(text string) error {
	switch {
	case text == "":
		return fmt.Errorf("bench: status 2")
	case text[0] == '!':
		return fmt.Errorf("bench: status 3")
	}
	return nil
}

// Fold is bench::fold: the callback is called once. The Go side takes it as a
// value, which is the direct Go equivalent of the std::function the C++ arms
// construct.
//
//go:noinline
func Fold(fn func(int32, int32) int32, a, b int32) int32 { return fn(a, b) }

// Add32 is the callback Fold is benchmarked with: two integers, one result,
// no work, so the round trip is what the benchmark measures.
func Add32(a, b int32) int32 { return a + b }

// Add32Fn is Add32 behind a func value: the Go baseline for the reentrant arm
// reaches its callback the way an ordinary Go caller would.
var Add32Fn = func(a, b int32) int32 { return a + b }

// Counter is bench::Counter in Go: the opaque object's owner is the allocator
// here, as it is in the C++ arms.
type Counter struct {
	value int64
}

// NewCounter is the constructor.
func NewCounter(start int64) *Counter { return &Counter{value: start} }

// Get is the noexcept accessor.
//
//go:noinline
func (c *Counter) Get() int64 { return c.value }

// Bump is the noexcept mutator.
func (c *Counter) Bump(by int64) { c.value += by }

// Close is the destructor. There is nothing to free in Go; the call exists so
// the benchmark compares like with like.
func (c *Counter) Close() {}
