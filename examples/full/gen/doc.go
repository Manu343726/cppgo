// Package metrics is the Go API of the metrics C++ library. cppgo generates it
// from include/metrics/metrics.hpp, and the generated files beside this one are
// the whole package: nothing here is hand-written except this comment.
//
// The types and functions below are the C++ declarations as a Go caller sees
// them. Histogram is opaque, with a constructor, a Close, methods and a static
// function; Point is a Go struct mirrored from a POD; a call that can fail
// returns an error; and Accumulator is generic, instantiated by the program
// that imports this package.
package metrics
