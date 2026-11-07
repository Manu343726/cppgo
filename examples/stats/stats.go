// Package stats binds the case a Go program actually calls out to C++ for:
// arithmetic over a large []float64.
//
// Every function here takes its data as (pointer, length) — the two registers a
// slice already is — so a million elements cost the same to hand over as three,
// and the results come back through the caller's own slice instead of a return
// register. This file is rewritten in place by `cppgo gen`: each declaration
// gains a one-line body that delegates to the generated wrapper.
package stats

//cppgo:library libstats
//cppgo:header include/stats/stats.hpp

// Mean returns the mean of xs, or 0 for an empty slice.
//
//cppgo:function stats::mean
func Mean(xs []float64) float64 { return cppgoCallMean(xs) }

// Stddev returns the population standard deviation of xs, or 0 for an empty
// slice.
//
//cppgo:function stats::stddev
func Stddev(xs []float64) float64 { return cppgoCallStddev(xs) }

// CountAbove counts the values strictly greater than threshold.
//
//cppgo:function stats::count_above
func CountAbove(xs []float64, threshold float64) int { return cppgoCallCountAbove(xs, threshold) }

// DataPointer returns a pointer to the first element of xs, straight from the
// C++ side. It exists to prove the point in a test and a benchmark: the pointer
// that arrives is the pointer that left, so the slice was not copied.
//
//cppgo:function stats::data_pointer
func DataPointer(xs []float64) *float64 { return cppgoCallDataPointer(xs) }

// Histogram counts xs into len(bins) buckets spanning [lo, hi] and writes the
// counts into bins, which is the caller's own slice: the output crosses as a
// (pointer, length) pair as well, so nothing is marshalled back either.
//
// A direct C++ call has no status channel of its own, so a binding that can
// fail declares only an error result: the C++ function returns an int32_t
// status and the wrapper turns a non-zero one into a Go error, reading the
// message from the library's cppgo_last_error.
//
//cppgo:function stats::histogram
func Histogram(xs, bins []float64, lo, hi float64) error { return cppgoCallHistogram(xs, bins, lo, hi) }
