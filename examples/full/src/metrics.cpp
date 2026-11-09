// metrics' translation unit. It compiles into libmetrics.so with the project's
// own CMake build (see CMakeLists.txt); no cppgo-generated file is part of it.
#include "metrics/metrics.hpp"

#include <cmath>
#include <stdexcept>

namespace metrics {

namespace {

// uniform_bounds is the bucket edges of a histogram over [0, 1]: buckets + 1
// values from 0 to 1 inclusive.
std::vector<double> uniform_bounds(std::size_t buckets) {
    if (buckets == 0) {
        throw std::invalid_argument("a histogram needs at least one bucket");
    }
    std::vector<double> bounds;
    bounds.reserve(buckets + 1);
    for (std::size_t i = 0; i <= buckets; ++i) {
        bounds.push_back(static_cast<double>(i) / static_cast<double>(buckets));
    }
    return bounds;
}

// validates_name is the rule validate_name reports as a status code.
std::int32_t validates_name(const std::string &name) {
    if (name.empty()) {
        return 1;
    }
    for (const char c : name) {
        const bool ok = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
                        (c >= '0' && c <= '9') || c == '_' || c == '-';
        if (!ok) {
            return 2;
        }
    }
    return 0;
}

} // namespace

Histogram::Histogram(std::string name, std::size_t buckets)
    : name_(std::move(name)), bounds_(uniform_bounds(buckets)),
      counts_(buckets, 0), total_(0), sum_(0.0) {}

Histogram::Histogram(const Histogram &other) = default;

Histogram::~Histogram() = default;

std::size_t Histogram::buckets() const noexcept { return counts_.size(); }

double Histogram::total() const noexcept { return static_cast<double>(total_); }

std::int32_t Histogram::version() noexcept { return 4; }

void Histogram::observe(double value) {
    if (!std::isfinite(value)) {
        throw std::invalid_argument("a sample must be finite");
    }
    if (value < 0.0 || value > 1.0) {
        throw std::out_of_range("a sample must lie in [0, 1]");
    }
    // The last bucket owns the right edge, so 1.0 is counted.
    std::size_t bucket = counts_.size() - 1;
    for (std::size_t i = 0; i < counts_.size(); ++i) {
        if (value < bounds_[i + 1]) {
            bucket = i;
            break;
        }
    }
    ++counts_[bucket];
    ++total_;
    sum_ += value;
}

double Histogram::mean() const {
    if (total_ == 0) {
        throw std::runtime_error("the histogram is empty");
    }
    return sum_ / static_cast<double>(total_);
}

std::string Histogram::name() const { return name_; }

std::vector<double> Histogram::bounds() const { return bounds_; }

std::vector<std::size_t> Histogram::counts() const { return counts_; }

Point centroid(Point a, Point b) noexcept {
    return Point{(a.x + b.x) / 2.0, (a.y + b.y) / 2.0};
}

std::string describe(const std::string &prefix, const Point &p) {
    return prefix + "(" + std::to_string(p.x) + ", " + std::to_string(p.y) + ")";
}

std::vector<double> scaled(const std::vector<double> &values, double factor) {
    std::vector<double> out;
    out.reserve(values.size());
    for (const double value : values) {
        out.push_back(value * factor);
    }
    return out;
}

double ratio(double numerator, double denominator) {
    if (denominator == 0.0) {
        throw std::invalid_argument("division by zero");
    }
    return numerator / denominator;
}

std::int32_t validate_name(const std::string &name) { return validates_name(name); }

double apply(double (*fn)(double), double value) { return fn(value); }

} // namespace metrics
