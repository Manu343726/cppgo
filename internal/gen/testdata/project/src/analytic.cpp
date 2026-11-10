// The project's own translation unit. It compiles into libanalytic.so with the
// project's own command (see Makefile); no cppgo-generated file is part of it.
#include "analytic/analytic.hpp"

#include <cmath>
#include <numeric>
#include <stdexcept>

namespace analytic {

Signal::Signal(double gain, std::size_t taps) : gain_(gain), taps_(taps) {}

Signal::Signal(const Signal &other) : gain_(other.gain_ + 1), taps_(other.taps_) {}

Signal::~Signal() {}

double Signal::energy() const { return gain_ * static_cast<double>(taps_); }

double Signal::scaled(double k) const noexcept { return gain_ * k; }

std::int32_t Signal::apply(const std::function<std::int32_t(std::int32_t)> &fn,
                           std::int32_t value) const {
    return fn(value) + static_cast<std::int32_t>(gain_);
}

double Signal::dot(const double *values, std::size_t n) const {
    double sum = 0.0;
    for (std::size_t i = 0; i < n && i < taps_; ++i) {
        sum += gain_ * values[i];
    }
    return sum;
}

std::int32_t Signal::version() { return 4; }

Point midpoint(Point a, Point b) { return Point{(a.x + b.x) / 2.0, (a.y + b.y) / 2.0}; }

Sample blend(Sample a, Sample b, double t) noexcept {
    return Sample{a.a + (b.a - a.a) * t, a.b + (b.b - a.b) * t, a.c + (b.c - a.c) * t};
}

Point scaled_point(const Point &p, double k) noexcept { return Point{p.x * k, p.y * k}; }

std::string describe(const std::string &prefix, double value) {
    return prefix + "=" + std::to_string(value);
}

std::vector<double> window(std::size_t n) {
    std::vector<double> out;
    out.reserve(n);
    for (std::size_t i = 0; i < n; ++i) {
        out.push_back(static_cast<double>(i) / 2.0);
    }
    return out;
}

std::int32_t ratio(std::int32_t a, std::int32_t b) {
    if (b == 0) {
        throw std::invalid_argument("division by zero");
    }
    return a / b;
}

std::int32_t fold(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                  std::int32_t a, std::int32_t b) {
    return fn(a, b);
}

double integrate(double (*fn)(double), double from, double to) {
    // Two-point Gauss-Legendre on one interval: enough to prove the callback is
    // called with the arguments the Go side passed and its result read back.
    const double middle = (from + to) / 2.0;
    const double half = (to - from) / 2.0;
    const double offset = half / std::sqrt(3.0);
    return half * (fn(middle - offset) + fn(middle + offset));
}

double eval(double (*fn)(double), double x) noexcept { return fn(x); }

bool positive(double (*fn)(double), double x) noexcept { return fn(x) > 0; }

std::uint32_t tally(std::uint32_t (*fn)(std::uint32_t), std::uint32_t n) noexcept {
    std::uint32_t sum = 0;
    for (std::uint32_t i = 1; i <= n; ++i) {
        sum += fn(i);
    }
    return sum;
}

void visit(void (*fn)(double), std::size_t n) {
    for (std::size_t i = 0; i < n; ++i) {
        fn(static_cast<double>(i));
    }
}

double pair(double (*fn)(double), double (*first)(double), double x) {
    return fn(first(x));
}

bool truthy(bool (*fn)(bool), bool value) noexcept { return fn(value); }

Sample cb_blend(double (*fn)(double), double t) noexcept {
    const double value = fn(t);
    return Sample{value, value * 2, value * 3};
}

namespace {

// The records the payload callbacks deliver. They live for the whole process,
// which is what lets the Go side read them during the callback and compare
// them with a copy afterwards.
const char *const kMessages[] = {"hello", "mundo", "h\xc3\xa9llo w\xc3\xb6rld"};

const std::uint8_t kBytes[] = {0x00, 0x01, 0x7f, 0x80, 0xff};

} // namespace

void deliver_messages(MessageCallback fn) {
    if (fn == nullptr) {
        return;
    }
    for (std::int32_t level = 0; level < 3; ++level) {
        fn(level, kMessages[level]);
    }
}

void deliver_bytes(void (*fn)(const std::uint8_t *data, std::size_t n)) {
    if (fn != nullptr) {
        fn(kBytes, sizeof kBytes);
    }
}

void deliver_point(void (*fn)(const Point *point)) {
    const Point held{3, 4};
    if (fn != nullptr) {
        fn(&held);
    }
}

std::int32_t checksum(const std::uint8_t *data, std::size_t n, void (*fn)(std::int32_t count)) {
    std::uint32_t before = 0;
    for (std::size_t i = 0; i < n; ++i) {
        before += data[i];
    }
    if (fn != nullptr) {
        fn(static_cast<std::int32_t>(n));
    }
    std::uint32_t after = 0;
    for (std::size_t i = 0; i < n; ++i) {
        after += data[i];
    }
    if (before != after) {
        return -1;
    }
    return static_cast<std::int32_t>(before);
}

void bump(Point *point, void (*fn)(std::int32_t value)) {
    if (point == nullptr) {
        return;
    }
    if (fn != nullptr) {
        fn(static_cast<std::int32_t>(point->x));
    }
    point->x += 10;
    point->y += 20;
}

} // namespace analytic
