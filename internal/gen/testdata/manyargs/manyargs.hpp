#pragma once

#include <cstdint>
#include <string>

// The argument-block fixture: signatures the register file cannot carry, one
// per shape the block has to get right.
//
// Every function's result is a positional encoding of its arguments (each
// argument multiplied by a distinct power of ten), so an argument that arrives
// in the wrong slot cannot produce the expected number. The std::string case is
// the shape Eigen's IOFormat constructor has: two integers and six strings is
// fourteen INTEGER words, and the shim builds each string from a word pair that
// lives in the block.
namespace many {

/// ints needs eight INTEGER words; the file carries six.
std::int64_t ints(std::int64_t a, std::int64_t b, std::int64_t c, std::int64_t d,
                  std::int64_t e, std::int64_t f, std::int64_t g, std::int64_t h) noexcept;

/// floats needs nine SSE words; the file carries eight.
double floats(double a, double b, double c, double d, double e, double f, double g, double h,
              double i) noexcept;

/// mixed interleaves the two classes and starts with a four-byte word, so the
/// block's padding after `first` is part of what the offsets assert.
double mixed(float first, std::int64_t i1, double d1, std::int64_t i2, double d2,
             std::int64_t i3, double d3, std::int64_t i4, double d4, std::int64_t i5,
             double d5, std::int64_t i6, double d6, std::int64_t i7) noexcept;

/// strings is the IOFormat shape: fourteen INTEGER words, six of them pairs the
/// shim turns into std::string locals.
std::int64_t strings(std::int32_t precision, std::int32_t flags, const std::string &a,
                     const std::string &b, const std::string &c, const std::string &d,
                     const std::string &e, const std::string &f) noexcept;

/// Wide's constructor needs eight INTEGER words — seven arguments plus its
/// this — so the factory crosses in the block too, and `new (self)` reads the
/// receiver out of it.
class Wide {
public:
    Wide(std::int64_t a, std::int64_t b, std::int64_t c, std::int64_t d, std::int64_t e,
         std::int64_t f, std::int64_t g) noexcept;
    std::int64_t value() const noexcept;

private:
    std::int64_t value_;
};

/// The receiver is an argument word like any other: a method with six arguments
/// is seven INTEGER words with its this.
class Accumulator {
public:
    Accumulator() noexcept;
    explicit Accumulator(std::int64_t base) noexcept;
    std::int64_t add(std::int64_t a, std::int64_t b, std::int64_t c, std::int64_t d,
                     std::int64_t e, std::int64_t f) noexcept;
    std::int64_t value() const noexcept;

private:
    std::int64_t value_;
};

} // namespace many
