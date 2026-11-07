package stats

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The bindings open the library named by //cppgo:library ("libstats"), which the
// dynamic loader looks up on its search path, not in the current directory. The
// test points them at the file the Makefile built; CppgoLibraryPath is read
// once, on the first call, which is why this runs in init.
func init() {
	path := os.Getenv("CPPGO_STATS_LIB")
	if path == "" {
		path = filepath.Join("build", "libstats.so")
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	CppgoLibraryPath = path
}

func TestMain(m *testing.M) {
	if _, err := os.Stat(CppgoLibraryPath); err != nil {
		fmt.Fprintf(os.Stderr, "libstats.so is missing: %v\nrun \"make\" in this directory first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// close reports whether two doubles agree to a relative 1e-12. The results come
// from a different compiler and a different summation order, so exact equality
// would be asserting on floating point rather than on the binding.
func close(got, want float64) bool {
	scale := math.Abs(want)
	if scale < 1 {
		scale = 1
	}
	return math.Abs(got-want) <= 1e-12*scale
}

// TestMean covers the scalar cases and then the case that matters: a slice
// large enough that the (pointer, length) hand-off is the whole point.
func TestMean(t *testing.T) {
	tests := []struct {
		name string
		xs   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{7}, 7},
		{"four", []float64{1, 2, 3, 4}, 2.5},
		{"negative", []float64{-3, 0, 3}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Mean(tc.xs); !close(got, tc.want) {
				t.Errorf("Mean(%v) = %v, want %v", tc.xs, got, tc.want)
			}
		})
	}
}

// TestStddev checks the population standard deviation, whose well-known value
// for the classic eight-number series is exactly 2.
func TestStddev(t *testing.T) {
	xs := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	if got := Mean(xs); !close(got, 5) {
		t.Errorf("Mean(%v) = %v, want 5", xs, got)
	}
	if got := Stddev(xs); !close(got, 2) {
		t.Errorf("Stddev(%v) = %v, want 2", xs, got)
	}
	if got := Stddev(nil); got != 0 {
		t.Errorf("Stddev(nil) = %v, want 0", got)
	}
}

func TestCountAbove(t *testing.T) {
	xs := []float64{1, 5, 2, 5, 3}
	if got := CountAbove(xs, 2); got != 3 {
		t.Errorf("CountAbove(%v, 2) = %d, want 3", xs, got)
	}
	// Strictly greater: the two 5s count, the 2 does not.
	if got := CountAbove(xs, 5); got != 0 {
		t.Errorf("CountAbove(%v, 5) = %d, want 0", xs, got)
	}
	if got := CountAbove(nil, 0); got != 0 {
		t.Errorf("CountAbove(nil, 0) = %d, want 0", got)
	}
}

// TestHistogramWritesThroughTheSlice is the output half of the no-copy story:
// the C++ side is handed the Go slice's backing array and fills it in place.
func TestHistogramWritesThroughTheSlice(t *testing.T) {
	// Four buckets of width 2 over [0, 8]: 0,1 | 2,3 | 4,5 | 6,7,8.
	xs := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8}
	bins := make([]float64, 4)

	if err := Histogram(xs, bins, 0, 8); err != nil {
		t.Fatalf("Histogram: %v", err)
	}

	want := []float64{2, 2, 2, 3} // the right edge, x == hi, belongs to the last bucket
	if !reflect.DeepEqual(bins, want) {
		t.Errorf("bins = %v, want %v", bins, want)
	}

	// Calling again starts from zero: the C++ fill() is what clears the slice,
	// so a stale count would mean the write never reached Go memory.
	if err := Histogram([]float64{4}, bins, 0, 8); err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	if want := []float64{0, 0, 1, 0}; !reflect.DeepEqual(bins, want) {
		t.Errorf("bins after the second call = %v, want %v", bins, want)
	}
}

// TestHistogramErrorPath is the one place where a direct C++ call reports
// failure: an integer status, plus a message the wrapper reads back through
// cppgo_last_error.
func TestHistogramErrorPath(t *testing.T) {
	err := Histogram([]float64{1, 2, 3}, make([]float64, 4), 8, 0)
	if err == nil {
		t.Fatal("Histogram with hi < lo returned no error")
	}
	if !strings.Contains(err.Error(), "hi must be greater than lo") {
		t.Errorf("error = %q, want it to carry the message stored by the C++ side", err)
	}
	// The wrapper prefixes the qualified C++ name, so the message says which
	// binding failed.
	if !strings.Contains(err.Error(), "stats::histogram") {
		t.Errorf("error = %q, want it to name stats::histogram", err)
	}

	// An empty output slice is a different rejection, from the same convention.
	if err := Histogram([]float64{1}, nil, 0, 1); err == nil {
		t.Error("Histogram with no bins returned no error")
	} else if !strings.Contains(err.Error(), "nbins must be positive") {
		t.Errorf("error = %q, want it to mention nbins", err)
	}
}

// TestSliceIsNotCopied is the proof the tutorial can only assert: the C++ side
// returns the pointer it received, and it is the address of the caller's first
// element. Writing through it changes the Go slice, because it is Go memory.
func TestSliceIsNotCopied(t *testing.T) {
	xs := []float64{1, 2, 3, 4}

	got := DataPointer(xs)
	if got == nil {
		t.Fatal("DataPointer returned nil")
	}
	if got != &xs[0] {
		t.Fatalf("the C++ side saw %p, but the Go slice starts at %p: the slice was copied", got, &xs[0])
	}

	*got = 100
	if xs[0] != 100 {
		t.Fatalf("writing through the returned pointer left xs[0] = %v: not the same memory", xs[0])
	}

	// An empty slice has no first element: cppgo.Ptr returns nil rather than a
	// pointer to nowhere.
	if got := DataPointer(nil); got != nil {
		t.Errorf("DataPointer(nil) = %p, want nil", got)
	}
}

// TestLargeInput runs the real workload: a million elements, compared against
// the pure-Go implementation in this file.
func TestLargeInput(t *testing.T) {
	xs := make([]float64, 1<<20)
	for i := range xs {
		xs[i] = math.Sin(float64(i) * 0.001)
	}

	if got, want := Mean(xs), goMean(xs); !close(got, want) {
		t.Errorf("Mean over %d elements = %v, want %v", len(xs), got, want)
	}
	if got, want := Stddev(xs), goStddev(xs); !close(got, want) {
		t.Errorf("Stddev over %d elements = %v, want %v", len(xs), got, want)
	}
	if got, want := CountAbove(xs, 0.5), goCountAbove(xs, 0.5); got != want {
		t.Errorf("CountAbove over %d elements = %d, want %d", len(xs), got, want)
	}

	bins := make([]float64, 16)
	wantBins := make([]float64, 16)
	if err := Histogram(xs, bins, -1, 1); err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	goHistogram(xs, wantBins, -1, 1)
	if !reflect.DeepEqual(bins, wantBins) {
		t.Errorf("Histogram over %d elements = %v, want %v", len(xs), bins, wantBins)
	}

	var total float64
	for _, b := range bins {
		total += b
	}
	if total != float64(len(xs)) {
		t.Errorf("the histogram accounts for %v of %d values", total, len(xs))
	}
}

// The pure-Go implementations the benchmark and TestLargeInput compare against.
// They exist so the comparison is against something a reader can see, not
// against a number in a README.

func goMean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func goStddev(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	mean := goMean(xs)
	var sum float64
	for _, x := range xs {
		d := x - mean
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(xs)))
}

func goCountAbove(xs []float64, threshold float64) int {
	count := 0
	for _, x := range xs {
		if x > threshold {
			count++
		}
	}
	return count
}

func goHistogram(xs, bins []float64, lo, hi float64) {
	if len(bins) == 0 || !(hi > lo) {
		return
	}
	for i := range bins {
		bins[i] = 0
	}
	width := (hi - lo) / float64(len(bins))
	for _, x := range xs {
		if !(x >= lo && x <= hi) {
			continue
		}
		bucket := int((x - lo) / width)
		if bucket >= len(bins) {
			bucket = len(bins) - 1
		}
		bins[bucket]++
	}
}
