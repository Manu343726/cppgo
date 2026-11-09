// The argument-block fixture's implementation. Each result is the positional
// encoding described in the header: argument k is multiplied by 10^k, so a
// value that arrives in the wrong slot changes the number.
#include "manyargs.hpp"

namespace many {

std::int64_t ints(std::int64_t a, std::int64_t b, std::int64_t c, std::int64_t d,
                  std::int64_t e, std::int64_t f, std::int64_t g, std::int64_t h) noexcept {
    return a + b * 10 + c * 100 + d * 1000 + e * 10000 + f * 100000 + g * 1000000 +
           h * 10000000;
}

double floats(double a, double b, double c, double d, double e, double f, double g, double h,
              double i) noexcept {
    return a + b * 10 + c * 100 + d * 1000 + e * 10000 + f * 100000 + g * 1000000 +
           h * 10000000 + i * 100000000;
}

double mixed(float first, std::int64_t i1, double d1, std::int64_t i2, double d2,
             std::int64_t i3, double d3, std::int64_t i4, double d4, std::int64_t i5,
             double d5, std::int64_t i6, double d6, std::int64_t i7) noexcept {
    return first + i1 * 10.0 + d1 * 100.0 + i2 * 1000.0 + d2 * 10000.0 + i3 * 100000.0 +
           d3 * 1000000.0 + i4 * 10000000.0 + d4 * 100000000.0 + i5 * 1000000000.0 +
           d5 * 10000000000.0 + i6 * 100000000000.0 + d6 * 1000000000000.0 +
           i7 * 10000000000000.0;
}

std::int64_t strings(std::int32_t precision, std::int32_t flags, const std::string &a,
                     const std::string &b, const std::string &c, const std::string &d,
                     const std::string &e, const std::string &f) noexcept {
    return precision + flags * 10 + static_cast<std::int64_t>(a.size()) * 100 +
           static_cast<std::int64_t>(b.size()) * 1000 +
           static_cast<std::int64_t>(c.size()) * 10000 +
           static_cast<std::int64_t>(d.size()) * 100000 +
           static_cast<std::int64_t>(e.size()) * 1000000 +
           static_cast<std::int64_t>(f.size()) * 10000000;
}

Wide::Wide(std::int64_t a, std::int64_t b, std::int64_t c, std::int64_t d, std::int64_t e,
           std::int64_t f, std::int64_t g) noexcept
    : value_(a + b * 10 + c * 100 + d * 1000 + e * 10000 + f * 100000 + g * 1000000) {}

std::int64_t Wide::value() const noexcept { return value_; }

Accumulator::Accumulator() noexcept : value_(0) {}

Accumulator::Accumulator(std::int64_t base) noexcept : value_(base) {}

std::int64_t Accumulator::add(std::int64_t a, std::int64_t b, std::int64_t c, std::int64_t d,
                              std::int64_t e, std::int64_t f) noexcept {
    value_ += a + b * 10 + c * 100 + d * 1000 + e * 10000 + f * 100000;
    return value_;
}

std::int64_t Accumulator::value() const noexcept { return value_; }

} // namespace many
