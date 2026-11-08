#pragma once

#include <cstddef>

// The phase 2a fixture: the shape DESIGN.md binds end to end. Vector is opaque
// (private members), Point is a mirrored POD, saxpy is the BLAS slice shape,
// scale is noexcept and out of line (direct carrier), and everything else is
// carried by a generated thunk.
namespace mathx {

struct Point {
    double x;
    double y;
};

class Vector {
public:
    Vector(double x, double y);
    Vector(const Vector &other);
    ~Vector();

    // Defined inside the class definition: implicitly inline, so the library
    // exports no symbol and the binding must go through a thunk.
    double x() const { return x_; }

    double norm() const;
    double dot(const double *ys, std::size_t n) const;
    void scale(double k) noexcept;
    static int dims();

private:
    double x_;
    double y_;
};

void saxpy(double a, const double *x, double *y, std::size_t n);

} // namespace mathx
