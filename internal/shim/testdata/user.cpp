// The user's own translation unit: the definitions their build compiles into
// the library. Nothing here is generated, and nothing here includes cppgo.
#include "user.hpp"

#include <functional>
#include <numeric>

namespace user {

Shape::~Shape() = default;
const char *Shape::name() const { return "Shape"; }

Circle::Circle(double r) : r_(r) {}
Circle::~Circle() = default;
double Circle::area() const { return 3.0 * r_ * r_; }
const char *Circle::name() const { return "Circle"; }
double Circle::radius() const { return r_; }

Serializable::Serializable(std::int32_t id) : id_(id) {}
Serializable::~Serializable() = default;
std::int32_t Serializable::serial() const { return id_; }

namespace {
std::int32_t g_disk_destroys = 0;
}

Disk::Disk(std::int32_t id, double r) : Serializable(id), Circle(r), tag_(id * 10) {}
Disk::~Disk() { ++g_disk_destroys; }
std::int32_t disk_destroys() { return g_disk_destroys; }
std::int32_t Disk::serial() const { return tag_; }

Tiny::Tiny(std::int32_t value) : value_(value) {}
Tiny::Tiny(const Tiny &other) : value_(other.value_ + 1000) {}
Tiny::~Tiny() = default;
std::int32_t Tiny::value() const { return value_; }

std::string greet(const std::string &prefix, const std::string &name) {
    return prefix + ", " + name + "!";
}

std::vector<double> doubled(const std::vector<double> &values) {
    std::vector<double> out;
    out.reserve(values.size());
    for (double v : values) {
        out.push_back(v * 2);
    }
    return out;
}

double sum(const std::vector<double> &values) {
    return std::accumulate(values.begin(), values.end(), 0.0);
}

std::int32_t checked_div(std::int32_t a, std::int32_t b) {
    if (b == 0) {
        throw std::invalid_argument("division by zero");
    }
    return a / b;
}

std::int32_t apply(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                   std::int32_t a, std::int32_t b) {
    return fn(a, b);
}

} // namespace user
