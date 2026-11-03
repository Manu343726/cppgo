#include "mathx.hpp"

#include <cmath>

namespace mathx {

Vector::Vector(double x, double y) : x_(x), y_(y) {}
Vector::~Vector() {}

double Vector::norm() const { return std::sqrt(x_ * x_ + y_ * y_); }

double Vector::dot(const double *ys, std::size_t n) const {
    const double components[2] = {x_, y_};
    double sum = 0.0;
    if (n > 2) {
        n = 2;
    }
    for (std::size_t i = 0; i < n; ++i) {
        sum += components[i] * ys[i];
    }
    return sum;
}

Vector Vector::scaled(double k) const { return Vector(x_ * k, y_ * k); }

double Vector::scale() const { return 1.0; }

int Vector::dims() { return 2; }

int Vector::pick(int Vector::*member) { return 0; }

const char *Vector::name() { return "Vector"; }

Shape::Shape(int id) : id_(id) {}
Shape::~Shape() {}

int Shape::id() const { return id_; }

Tiny::~Tiny() {}

Level6::~Level6() {}

Level1 make_deep() { return Level1{}; }

Vector make(double x, double y) { return Vector(x, y); }

Tiny make_tiny(int value) { return Tiny{value}; }

Pair make_pair(double a, double b) { return Pair{a, b}; }

void SmallVirtual::touch() {}

SmallCopy::SmallCopy() : value(0) {}

SmallCopy::SmallCopy(const SmallCopy &other) : value(other.value) {}

SmallMove::SmallMove() : value(0) {}

SmallMove::SmallMove(SmallMove &&other) : value(other.value) {}

SmallVirtual make_small_virtual(int value) {
    SmallVirtual result;
    result.value = value;
    return result;
}

SmallCopy make_small_copy(int value) {
    SmallCopy original;
    original.value = value;
    return SmallCopy(original);
}

SmallMove make_small_move(int value) {
    SmallMove original;
    original.value = value;
    return SmallMove(static_cast<SmallMove &&>(original));
}

SmallVirtualBase make_small_virtual_base(int value) {
    SmallVirtualBase result;
    result.value = value;
    return result;
}

SmallMember make_small_member(int value) {
    SmallMember result;
    result.inner.value = value;
    return result;
}

bool is_axis(Axis axis, bool strict) {
    if (strict) {
        return axis == Axis::X;
    }
    return axis == Axis::X || axis == Axis::Y;
}

int add(int a, int b) { return a + b; }

double add(double a, double b) { return a + b; }

static int internal_counter() { return 0; }

namespace {
void hidden_helper() {}
} // namespace

} // namespace mathx
