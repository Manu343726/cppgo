// Spelled "vector.hpp" with include/mathx on the include path (see the
// Makefile) rather than <mathx/vector.hpp>. cppgo discovers the translation
// units that carry the out-of-line bodies by looking for a source that includes
// the header by its file name, so the bare form is what makes the definitions
// in this file visible to the generator instead of every member looking inline.
#include "vector.hpp"

#include <cmath>

namespace mathx {

// Every definition in this file is out of line on purpose: it is what gives
// each member a symbol in the shared object, and the symbol is what cppgo
// binds. `norm_squared` is not here — it lives in the header, inline, and that
// is precisely why it cannot be bound.

Vector::Vector(double x, double y) : x_(x), y_(y) {}

// Spelled `{}` rather than `= default`: an explicitly defaulted definition is
// still fine out of line, but `= default` inside the class would be inline.
Vector::~Vector() {}

double Vector::x() const { return x_; }

double Vector::y() const { return y_; }

double Vector::norm() const { return std::sqrt(x_ * x_ + y_ * y_); }

double Vector::dot(const double *ys, std::size_t n) const {
    // A slice is a borrowed pointer plus a length. Nothing here checks a Go
    // bound: n is whatever the caller passed, so this is the function that has
    // to be honest about it.
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

void Vector::scale(double k) {
    x_ *= k;
    y_ *= k;
}

int Vector::dims() { return 2; }

} // namespace mathx
