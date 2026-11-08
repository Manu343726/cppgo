#pragma once

// stats — the slice-heavy half of the v4 example.
//
// Nothing here knows that cppgo exists. The declarations below are the source
// of truth: `cppgo gen` reads them through libclang and assigns each one a
// carrier, which the report it prints names.
//
// The split in this header is deliberate and is the lesson the example teaches:
//
//   - mean, stddev, count_above and data_pointer are `noexcept` and defined
//     out of line, so they are called directly through their mangled symbols
//     and their Go signatures have no error result;
//   - histogram is not noexcept, so it goes through the shim, which catches
//     what it throws and returns the message as a Go error.
//
// Every function takes its data as (pointer, length), which is exactly what a
// Go slice is on the wire. Nothing is copied in either direction, including
// the output array histogram writes into.

#include <cstddef>

namespace stats {

// One spelling for the length half of every (pointer, length) pair. A slice
// spends two C++ parameters — pointer then length — at its own position in the
// declaration.
using std::size_t;

// mean of n values, or 0 for n == 0.
double mean(const double *xs, size_t n) noexcept;

// stddev is the population standard deviation (divide by n) of n values, or 0
// for n == 0.
double stddev(const double *xs, size_t n) noexcept;

// count_above counts the values strictly greater than threshold.
size_t count_above(const double *xs, size_t n, double threshold) noexcept;

// data_pointer returns the pointer it was given. If the slice had been copied
// on the way in, this would not be the address of the caller's first element.
const double *data_pointer(const double *xs, size_t n) noexcept;

// histogram counts the values into nbins buckets spanning [lo, hi] and writes
// the counts straight into the caller's array: the output crosses as a
// (pointer, length) pair too. Values outside [lo, hi] (and NaNs) are dropped.
//
// It reports failure by throwing std::invalid_argument. The shim catches it, so
// the generated Go signature is just an error — the C++ function has no status
// return to explain and none appears in Go.
void histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo,
               double hi);

} // namespace stats
