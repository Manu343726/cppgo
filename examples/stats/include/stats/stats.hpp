#pragma once

#include <cstddef>
#include <cstdint>

namespace stats {

// One spelling for the length half of every (pointer, length) pair. The
// using-declaration keeps `size_t` unqualified in the signatures below; the
// generator reports the result type as written, and `std::size_t` would be
// rejected by the same rule that rejects std::string and std::vector.
using std::int32_t;
using std::size_t;

// Every function in this header takes its data as (pointer, length). That is
// exactly what a Go []float64 is on the wire, so a slice of a million elements
// costs the same two registers to hand over as a slice of three: nothing is
// copied, marshalled or serialised. The pointer is borrowed for the duration of
// the call and must not be stored.
//
// All of these are defined out of line in src/stats.cpp, which is what gives
// them a symbol for dlsym to find.

// mean of n values, or 0 for n == 0.
double mean(const double *xs, size_t n);

// stddev is the population standard deviation (divide by n) of n values, or 0
// for n == 0.
double stddev(const double *xs, size_t n);

// count_above counts the values strictly greater than threshold.
size_t count_above(const double *xs, size_t n, double threshold);

// data_pointer returns the pointer it was given. If the slice had been copied
// on the way in, this would not be the address of the caller's first element.
const double *data_pointer(const double *xs, size_t n);

// histogram counts the values into nbins buckets spanning [lo, hi] and writes
// the counts straight into the caller's array: the output crosses as a (pointer,
// length) pair too, so the results do not make a trip back through a return
// register either. Values outside [lo, hi] (and NaNs) are dropped.
//
// It returns an int32_t status: 0 for success, non-zero for a failure described
// by cppgo_last_error(). This is the error convention of a library that has no
// extern "C" shim to catch an exception for it — see the README.
int32_t histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo,
                       double hi);

} // namespace stats

// cppgo_last_error is optional, and the one symbol in this library that is
// deliberately extern "C": a call cannot report a message through a return
// register, so the generated wrapper looks this name up after a non-zero status
// and turns it into a Go error.
extern "C" const char *cppgo_last_error();
