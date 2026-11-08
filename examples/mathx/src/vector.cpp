// The definitions of mathx. Together with the header this is the user's
// library: the Makefile compiles it into build/libmathx.so with the project's
// own flags and its own soname. No cppgo-generated file is part of this build;
// cppgo's shim links *against* this library from the outside.
#include <mathx/vector.hpp>

#include <cmath>
#include <stdexcept>

namespace mathx {

Vector::Vector(double x, double y) : x_(x), y_(y) {}

Vector::Vector(const Vector &other) : x_(other.x_), y_(other.y_) {}

Vector::~Vector() {}

double Vector::y() const { return y_; }

double Vector::norm() const { return std::sqrt(x_ * x_ + y_ * y_); }

double Vector::dot(const double *ys, std::size_t n) const {
    // A slice is a borrowed pointer plus a length. Nothing here checks a Go
    // bound: n is whatever the caller passed, so this function is the one that
    // has to be honest about it.
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

double Vector::at(std::size_t index) const {
    if (index >= 2) {
        throw std::out_of_range("Vector::at: index out of range");
    }
    return index == 0 ? x_ : y_;
}

void Vector::validate() const {
    if (x_ == 0.0 && y_ == 0.0) {
        throw std::runtime_error("Vector::validate: the zero vector has no direction");
    }
}

void Vector::scale(double k) noexcept {
    x_ *= k;
    y_ *= k;
}

int Vector::dims() { return 2; }

Point midpoint(Point a, Point b) {
    return Point{(a.x + b.x) / 2.0, (a.y + b.y) / 2.0};
}

void saxpy(double a, const double *x, double *y, std::size_t n) {
    for (std::size_t i = 0; i < n; ++i) {
        y[i] = a * x[i] + y[i];
    }
}

} // namespace mathx
