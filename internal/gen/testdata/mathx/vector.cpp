#include "vector.hpp"

#include <cmath>

namespace mathx {

Vector::Vector(double x, double y) : x_(x), y_(y) {}

Vector::Vector(const Vector &other) : x_(other.x_), y_(other.y_) {}

Vector::~Vector() {}

double Vector::norm() const { return std::sqrt(x_ * x_ + y_ * y_); }

double Vector::dot(const double *ys, std::size_t n) const {
    const double components[2] = {x_, y_};
    if (n > 2) {
        n = 2;
    }
    double sum = 0.0;
    for (std::size_t i = 0; i < n; ++i) {
        sum += components[i] * ys[i];
    }
    return sum;
}

void Vector::scale(double k) noexcept {
    x_ *= k;
    y_ *= k;
}

int Vector::dims() { return 2; }

void saxpy(double a, const double *x, double *y, std::size_t n) {
    for (std::size_t i = 0; i < n; ++i) {
        y[i] = a * x[i] + y[i];
    }
}

} // namespace mathx
