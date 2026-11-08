#pragma once

// A project laid out the way a real one is: the header under include/, the
// definitions under src/, and the shared object built by the project's own
// command. Nothing here knows that cppgo exists, and nothing cppgo generates is
// compiled into it.
//
// The shape is the one phase 4a measures end to end: an opaque class with
// private members, a POD passed by reference and by value (16 bytes and above),
// std::string and std::vector on both sides, a function that throws, and two
// kinds of callback.

#include <cstddef>
#include <cstdint>
#include <functional>
#include <stdexcept>
#include <string>
#include <vector>

namespace analytic {

// A mirrored POD: two doubles, 16 bytes, register class.
struct Point {
    double x;
    double y;
};

// A mirrored POD above 16 bytes: three doubles come back through memory.
struct Sample {
    double a;
    double b;
    double c;
};

class Signal {
public:
    Signal(double gain, std::size_t taps);
    Signal(const Signal &other);
    ~Signal();

    // Defined inside the class: implicitly inline, so the library exports no
    // symbol and the binding has to reach the shim.
    double gain() const { return gain_; }

    // Out of line and not noexcept: the shim catches what it throws.
    double energy() const;

    // Out of line and noexcept: the direct carrier's symbol.
    double scaled(double k) const noexcept;

    // (pointer, length), which the Go side sees as a slice.
    double dot(const double *values, std::size_t n) const;

    // A method that takes a callback: the reentrant carrier with an implicit
    // this as the first INTEGER argument.
    std::int32_t apply(const std::function<std::int32_t(std::int32_t)> &fn,
                       std::int32_t value) const;

    static std::int32_t version();

private:
    double gain_;
    std::size_t taps_;
};

// A POD in and out, by value: 16 bytes, copied by the shim in both directions.
Point midpoint(Point a, Point b);

// A POD above 16 bytes in and out, noexcept: the result is written through the
// Go side's storage, and the call cannot fail.
Sample blend(Sample a, Sample b, double t) noexcept;

// A POD by reference, noexcept and out of line: the direct carrier with a
// record argument.
Point scaled_point(const Point &p, double k) noexcept;

// std::string and std::vector, constructed and copied by the shim.
std::string describe(const std::string &prefix, double value);
std::vector<double> window(std::size_t n);

// Throws std::invalid_argument on a zero divisor.
std::int32_t ratio(std::int32_t a, std::int32_t b);

// A std::function the shim builds from a Go function pointer.
std::int32_t fold(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                  std::int32_t a, std::int32_t b);

// A plain function pointer: the shim casts the callback address to it.
double integrate(double (*fn)(double), double from, double to);

// A noexcept callback: the reentrant carrier with its result in a register and
// no error channel at all.
double eval(double (*fn)(double), double x) noexcept;

// The same, with a bool result: the value comes back in AL.
bool positive(double (*fn)(double), double x) noexcept;

// And with an unsigned result, which travels back as the full register.
std::uint32_t tally(std::uint32_t (*fn)(std::uint32_t), std::uint32_t n) noexcept;

// A callback with no result: the Go function is called for its effect.
void visit(void (*fn)(double), std::size_t n);

// Two callbacks in one signature: two registrations, two releases, and the
// second address in the second INTEGER slot.
double pair(double (*fn)(double), double (*first)(double), double x);

// A bool argument and a bool result: the value occupies one byte of a register.
bool truthy(bool (*fn)(bool), bool value) noexcept;

// A noexcept callback whose own result is a record: no error channel, and the
// Sample is written through the storage the caller owns.
Sample cb_blend(double (*fn)(double), double t) noexcept;

} // namespace analytic
