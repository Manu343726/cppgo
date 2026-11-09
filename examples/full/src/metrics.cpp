// metrics' translation unit. It compiles into libmetrics.so with the project's
// own CMake build (see CMakeLists.txt); no cppgo-generated file is part of it.
#include "metrics/metrics.hpp"

#include <cmath>
#include <stdexcept>

namespace metrics {

// The state the header hides. It never crosses the Go boundary: Go holds a
// Histogram, and a Histogram holds this pointer.
struct Histogram::Impl {
    std::string name;
    std::vector<double> bounds;
    std::vector<std::size_t> counts;
    std::size_t total;
    double sum;
};

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
    : impl_(new Impl{std::move(name), uniform_bounds(buckets),
                     std::vector<std::size_t>(buckets, 0), 0, 0.0}) {}

Histogram::Histogram(const Histogram &other) : impl_(new Impl(*other.impl_)) {}

Histogram::~Histogram() { delete impl_; }

std::size_t Histogram::buckets() const noexcept { return impl_->counts.size(); }

double Histogram::total() const noexcept { return static_cast<double>(impl_->total); }

std::int32_t Histogram::version() noexcept { return 4; }

void Histogram::observe(double value) {
    if (!std::isfinite(value)) {
        throw std::invalid_argument("a sample must be finite");
    }
    if (value < 0.0 || value > 1.0) {
        throw std::out_of_range("a sample must lie in [0, 1]");
    }
    // The last bucket owns the right edge, so 1.0 is counted.
    std::size_t bucket = impl_->counts.size() - 1;
    for (std::size_t i = 0; i < impl_->counts.size(); ++i) {
        if (value < impl_->bounds[i + 1]) {
            bucket = i;
            break;
        }
    }
    ++impl_->counts[bucket];
    ++impl_->total;
    impl_->sum += value;
}

double Histogram::mean() const {
    if (impl_->total == 0) {
        throw std::runtime_error("the histogram is empty");
    }
    return impl_->sum / static_cast<double>(impl_->total);
}

std::string Histogram::name() const { return impl_->name; }

std::vector<double> Histogram::bounds() const { return impl_->bounds; }

std::vector<std::size_t> Histogram::counts() const { return impl_->counts; }

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
