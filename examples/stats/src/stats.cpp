// Spelled "stats.hpp" with include/stats on the include path (see the Makefile)
// so the generator finds this translation unit: cppgo discovers the files that
// carry the out-of-line definitions by looking for a source that includes the
// header by its file name.
#include "stats.hpp"

#include <algorithm>
#include <cmath>
#include <string>

namespace {

// The message of the last failed call on this thread, read back through
// cppgo_last_error. cppgo's contract is that it is valid until the next call on
// the same thread, which is exactly the lifetime of a std::string living here.
thread_local std::string last_error;

int32_t fail(const char *message) {
    last_error = message;
    return 1;
}

} // namespace

extern "C" const char *cppgo_last_error() { return last_error.c_str(); }

namespace stats {

double mean(const double *xs, size_t n) {
    if (n == 0) {
        return 0.0;
    }
    double sum = 0.0;
    for (size_t i = 0; i < n; ++i) {
        sum += xs[i];
    }
    return sum / static_cast<double>(n);
}

double stddev(const double *xs, size_t n) {
    if (n == 0) {
        return 0.0;
    }
    // Two passes: the mean first, then the deviations. One pass (sum of squares
    // minus square of the sum) is faster and numerically worse; this example is
    // about the boundary, not about numerical analysis.
    const double m = mean(xs, n);
    double sum = 0.0;
    for (size_t i = 0; i < n; ++i) {
        const double d = xs[i] - m;
        sum += d * d;
    }
    return std::sqrt(sum / static_cast<double>(n));
}

size_t count_above(const double *xs, size_t n, double threshold) {
    size_t count = 0;
    for (size_t i = 0; i < n; ++i) {
        if (xs[i] > threshold) {
            ++count;
        }
    }
    return count;
}

const double *data_pointer(const double *xs, size_t n) {
    (void)n;
    return xs;
}

int32_t histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo,
                       double hi) {
    // Validate once, then trust the values inside the loop: a per-element check
    // on a borrowed pointer is how a bulk call turns into a per-element one.
    if (nbins == 0) {
        return fail("histogram: nbins must be positive");
    }
    if (!(hi > lo)) {
        return fail("histogram: hi must be greater than lo");
    }
    if (n > 0 && xs == nullptr) {
        return fail("histogram: n > 0 but the input pointer is null");
    }
    if (bins == nullptr) {
        return fail("histogram: the output pointer is null");
    }

    // In-place initialisation of the caller's array: this is the same memory
    // the Go slice holds, so the caller sees these zeros and then the counts.
    std::fill(bins, bins + nbins, 0.0);

    const double width = (hi - lo) / static_cast<double>(nbins);
    for (size_t i = 0; i < n; ++i) {
        const double x = xs[i];
        // Written this way so NaN, which compares false against everything,
        // lands in the same branch as an out-of-range value instead of being
        // converted to an index.
        if (!(x >= lo && x <= hi)) {
            continue;
        }
        size_t bucket = static_cast<size_t>((x - lo) / width);
        if (bucket >= nbins) {
            bucket = nbins - 1; // the closed right edge, x == hi
        }
        bins[bucket] += 1.0;
    }
    return 0;
}

} // namespace stats
