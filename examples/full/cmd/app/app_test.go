package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Manu343726/cppgo"
	metrics "github.com/Manu343726/cppgo/examples/full/gen"
)

// testPath names one of the two objects relative to the example root. A test
// runs with its package's directory as the working directory, so the root is
// two levels up, whichever directory `go test` was started from.
func testPath(rel string) string {
	path := filepath.Join("..", "..", rel)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func TestMain(m *testing.M) {
	metrics.CppgoLibraryPath = testPath(filepath.Join("build", "libmetrics.so"))
	shim := os.Getenv("CPPGO_FULL_SHIM")
	if shim == "" {
		shim = testPath(filepath.Join("gen", "libcppgo_metrics.so"))
	}
	if err := metrics.Open(shim); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in the example root first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// approx compares doubles with a tolerance: a mean computed from C++ doubles
// is not required to match a Go expression bit for bit.
func approx(got, want float64) bool { return math.Abs(got-want) <= 1e-12 }

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !approx(a[i], b[i]) {
			return false
		}
	}
	return true
}

func equalUints(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestHistogramValues asserts the values of the opaque class: the constructor,
// the noexcept accessors, a method that cannot fail, and the std::string and
// std::vector results.
func TestHistogramValues(t *testing.T) {
	if got := metrics.HistogramVersion(); got != 4 {
		t.Fatalf("HistogramVersion() = %d, want 4", got)
	}

	h, err := metrics.NewHistogram("latency", 4)
	if err != nil {
		t.Fatalf("NewHistogram: %v", err)
	}
	defer h.Close()

	if got := h.Buckets(); got != 4 {
		t.Fatalf("Buckets() = %d, want 4", got)
	}
	if got := h.Total(); got != 0 {
		t.Fatalf("Total() = %v, want 0 on a fresh histogram", got)
	}

	for _, sample := range []float64{0.1, 0.6, 0.9} {
		if err := h.Observe(sample); err != nil {
			t.Fatalf("Observe(%v): %v", sample, err)
		}
	}
	if got := h.Total(); got != 3 {
		t.Fatalf("Total() = %v, want 3", got)
	}
	if err := h.Observe(1); err != nil {
		t.Fatalf("Observe(1): %v", err)
	}
	if got := h.Total(); got != 4 {
		t.Fatalf("Total() = %v after the right-edge sample, want 4", got)
	}

	name, err := h.Name()
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if name != "latency" {
		t.Fatalf("Name() = %q, want %q", name, "latency")
	}

	bounds, err := h.Bounds()
	if err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	if want := []float64{0, 0.25, 0.5, 0.75, 1}; !equalFloats(bounds, want) {
		t.Fatalf("Bounds() = %v, want %v", bounds, want)
	}

	counts, err := h.Counts()
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	// 1.0 lands in the last bucket, which owns the right edge.
	if want := []uint64{1, 0, 1, 2}; !equalUints(counts, want) {
		t.Fatalf("Counts() = %v, want %v", counts, want)
	}

	mean, err := h.Mean()
	if err != nil {
		t.Fatalf("Mean: %v", err)
	}
	if want := (0.1 + 0.6 + 0.9 + 1.0) / 4.0; !approx(mean, want) {
		t.Fatalf("Mean() = %v, want %v", mean, want)
	}
}

// TestCloneIsIndependent proves Clone goes through the C++ copy constructor:
// the copy has its own state.
func TestCloneIsIndependent(t *testing.T) {
	h, err := metrics.NewHistogram("clone", 2)
	if err != nil {
		t.Fatalf("NewHistogram: %v", err)
	}
	defer h.Close()
	if err := h.Observe(0.2); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	clone, err := h.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer clone.Close()
	if err := clone.Observe(0.8); err != nil {
		t.Fatalf("clone.Observe: %v", err)
	}
	if got := h.Total(); got != 1 {
		t.Fatalf("the original Total() = %v, want 1: the clone must not share state", got)
	}
	if got := clone.Total(); got != 2 {
		t.Fatalf("the clone Total() = %v, want 2", got)
	}

	if err := clone.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := clone.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Fatalf("a second Close = %v, want cppgo.ErrClosed", err)
	}
}

// TestPointByValue asserts the POD in and out: a 16-byte struct copied into
// the call and the result returned as a Go value.
func TestPointByValue(t *testing.T) {
	origin, err := metrics.NewPoint()
	if err != nil {
		t.Fatalf("NewPoint: %v", err)
	}
	corner := metrics.Point{X: 2, Y: 4}
	mid := metrics.Centroid(*origin, corner)
	if mid != (metrics.Point{X: 1, Y: 2}) {
		t.Fatalf("Centroid = %+v, want {X:1 Y:2}", mid)
	}

	described, err := metrics.Describe("corner", &corner)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !strings.HasPrefix(described, "corner(") || !strings.Contains(described, "2.0") {
		t.Fatalf("Describe = %q", described)
	}
}

// TestVectorCopiesInAndOut asserts that a slice is marshalled into a
// std::vector and the result into a fresh Go slice.
func TestVectorCopiesInAndOut(t *testing.T) {
	scaled, err := metrics.Scaled([]float64{1, 2, 3}, 2)
	if err != nil {
		t.Fatalf("Scaled: %v", err)
	}
	if want := []float64{2, 4, 6}; !equalFloats(scaled, want) {
		t.Fatalf("Scaled = %v, want %v", scaled, want)
	}
	if len(scaled) == 0 {
		t.Fatal("Scaled returned an empty slice")
	}

	empty, err := metrics.Scaled(nil, 3)
	if err != nil {
		t.Fatalf("Scaled(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Scaled(nil) = %v, want an empty slice", empty)
	}
}

// TestThrowingCall asserts the exception path: a C++ throw arrives as a Go
// error naming the declaration, and an infallible call has no error result.
func TestThrowingCall(t *testing.T) {
	value, err := metrics.Ratio(9, 4)
	if err != nil {
		t.Fatalf("Ratio(9, 4): %v", err)
	}
	if !approx(value, 2.25) {
		t.Fatalf("Ratio(9, 4) = %v, want 2.25", value)
	}

	_, err = metrics.Ratio(1, 0)
	if err == nil {
		t.Fatal("Ratio(1, 0) must fail")
	}
	var call *cppgo.CallError
	if !errors.As(err, &call) {
		t.Fatalf("Ratio(1, 0) = %T, want *cppgo.CallError", err)
	}
	if call.Symbol != "metrics::ratio" {
		t.Fatalf("the error names %q, want metrics::ratio", call.Symbol)
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("the error must carry the C++ message, got: %v", err)
	}

	// The same path covers a method that throws, including the marshalling.
	h, err := metrics.NewHistogram("throwing", 2)
	if err != nil {
		t.Fatalf("NewHistogram: %v", err)
	}
	defer h.Close()
	if _, err := h.Mean(); err == nil {
		t.Fatal("Mean on an empty histogram must fail")
	}
	if err := h.Observe(math.NaN()); err == nil {
		t.Fatal("Observe(NaN) must fail")
	}
	if err := h.Observe(-1); err == nil {
		t.Fatal("Observe(-1) must fail")
	}
}

// TestStatusCall asserts the -status convention: the Go signature returns
// only the error and a non-zero C++ status is the failure.
func TestStatusCall(t *testing.T) {
	if err := metrics.ValidateName("latency-1"); err != nil {
		t.Fatalf("ValidateName(latency-1) = %v, want nil: status 0 is success", err)
	}

	err := metrics.ValidateName("")
	if err == nil {
		t.Fatal("ValidateName(\"\") must fail")
	}
	var call *cppgo.CallError
	if !errors.As(err, &call) || call.Symbol != "metrics::validate_name" {
		t.Fatalf("ValidateName(\"\") = %v, want a CallError naming metrics::validate_name", err)
	}

	if err := metrics.ValidateName("not allowed"); err == nil {
		t.Fatal("ValidateName(\"not allowed\") must fail")
	}
}

// TestCallback asserts the C++ -> Go direction: the Go function runs inside
// the C++ call and its result comes back.
func TestCallback(t *testing.T) {
	var calls int
	got, err := metrics.Apply(func(v float64) float64 {
		calls++
		return v * 3
	}, 4)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != 12 {
		t.Fatalf("Apply(v*3, 4) = %v, want 12", got)
	}
	if calls != 1 {
		t.Fatalf("the Go function ran %d times, want 1", calls)
	}
}

// TestGenericInstantiations asserts the two-phase flow: the two Accumulator
// specializations and the function template the program type-checks are the
// ones the shim was built for.
func TestGenericInstantiations(t *testing.T) {
	i, err := metrics.NewAccumulator[int32](10)
	if err != nil {
		t.Fatalf("NewAccumulator[int32]: %v", err)
	}
	defer i.Close()
	if err := i.Add(5); err != nil {
		t.Fatalf("Add: %v", err)
	}
	sum, err := i.Total()
	if err != nil {
		t.Fatalf("Total: %v", err)
	}
	if sum != 15 {
		t.Fatalf("Accumulator[int32] total = %d, want 15", sum)
	}

	d, err := metrics.NewAccumulator[float64](1.5)
	if err != nil {
		t.Fatalf("NewAccumulator[float64]: %v", err)
	}
	defer d.Close()
	if err := d.Add(0.25); err != nil {
		t.Fatalf("Add: %v", err)
	}
	sumF, err := d.Total()
	if err != nil {
		t.Fatalf("Total: %v", err)
	}
	if !approx(sumF, 1.75) {
		t.Fatalf("Accumulator[float64] total = %v, want 1.75", sumF)
	}

	empty, err := metrics.NewAccumulatorDefault[float64]()
	if err != nil {
		t.Fatalf("NewAccumulatorDefault[float64]: %v", err)
	}
	if err := empty.Add(2); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, err := empty.Total(); err != nil || got != 2 {
		t.Fatalf("the default-constructed Accumulator total = %v, %v", got, err)
	}
	if err := empty.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	identity, err := metrics.AccumulatorIdentity[int32](7)
	if err != nil {
		t.Fatalf("AccumulatorIdentity: %v", err)
	}
	if identity != 7 {
		t.Fatalf("AccumulatorIdentity(7) = %d, want 7", identity)
	}

	twice, err := metrics.Twice[int64](21)
	if err != nil {
		t.Fatalf("Twice[int64]: %v", err)
	}
	if twice != 42 {
		t.Fatalf("Twice[int64](21) = %d, want 42", twice)
	}
}

// TestRun is the program itself: the report it prints must build and run.
func TestRun(t *testing.T) {
	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
	}
}
