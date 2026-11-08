// The definitions of bench. This file is compiled into build/libbench.so with
// the library's own flags and its own soname; no cppgo-generated file is part
// of that build, and the cgo shim links against the same object.
#include <bench/bench.hpp>

#include <cmath>
#include <cstdio>
#include <stdexcept>

namespace bench {

std::int64_t add(std::int64_t a, std::int64_t b) noexcept { return a + b; }

std::int64_t add_thunk(std::int64_t a, std::int64_t b) { return a + b; }

Point midpoint(Point a, Point b) {
    return Point{(a.x + b.x) / 2.0, (a.y + b.y) / 2.0};
}

Sample blend(Sample a, Sample b, double t) noexcept {
    return Sample{a.a + (b.a - a.a) * t, a.b + (b.b - a.b) * t, a.c + (b.c - a.c) * t};
}

std::string describe(std::string_view prefix, double value) {
    // std::to_string(double) is "%f": six decimals. The Go reference in
    // internal/benchdata asks strconv.FormatFloat for the same six, so the two
    // sides produce the same bytes and the tests can compare them.
    return std::string(prefix) + ": " + std::to_string(value);
}

std::vector<double> doubled(const std::vector<double> &values) {
    std::vector<double> result;
    result.reserve(values.size());
    for (const double value : values) {
        result.push_back(value * 2.0);
    }
    return result;
}

std::int32_t ratio(std::int32_t a, std::int32_t b) {
    if (b == 0) {
        throw std::domain_error("bench: division by zero");
    }
    return a / b;
}

std::int32_t parse(const std::string &text) {
    if (text.empty()) {
        return 2;
    }
    if (text.front() == '!') {
        return 3;
    }
    return 0;
}

std::int32_t fold(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                  std::int32_t a, std::int32_t b) {
    return fn(a, b);
}

Counter::Counter(std::int64_t start) : value_(start) {}

Counter::~Counter() = default;

std::int64_t Counter::get() const noexcept { return value_; }

void Counter::bump(std::int64_t by) noexcept { value_ += by; }

} // namespace bench
