#pragma once

// mathx — the C++ side of the v4 example.
//
// Nothing in this file, or in src/vector.cpp, knows that cppgo exists: there
// are no annotations, no extern "C" block and no binding declarations. This is
// an ordinary C++17 library, and its own Makefile target builds it into
// build/libmathx.so with the soname the cppgo-built shim links against.
//
// These declarations are the source of truth. `cppgo gen` reads them through
// libclang and chooses a carrier for every one of them:
//
//   - an out-of-line `noexcept` function whose arguments fit the System V
//     registers is called directly, through its mangled symbol;
//   - everything else — an inline body, a function that may throw, a record
//     passed by value — is called through the shim cppgo generates, compiles
//     and links against this library.
//
// The report `cppgo gen` prints names the carrier of every line, so the choice
// is auditable instead of implicit.

#include <cstddef>

namespace mathx {

// Point mirrors into Go as a struct with the same two fields: it is a 16-byte
// POD with a natural layout, so the Go value *is* the C++ layout. `midpoint`
// passes it by value through the shim.
struct Point {
    double x;
    double y;
};

class Vector {
public:
    Vector(double x, double y);

    // Declared and defined out of line like every other member. The generated
    // package binds it as Clone, and the definition in the source keeps the
    // copy observable to a reader.
    Vector(const Vector &other);

    ~Vector();

    // Defined inside the class body, therefore implicitly inline. That is no
    // longer a limitation: the shim includes this header, so the body is
    // compiled into the shim and X reaches it through a thunk.
    double x() const { return x_; }

    double y() const;

    // Out of line and not noexcept: the shim wraps it in try/catch, so the Go
    // signature is (float64, error).
    double norm() const;

    // A (pointer, length) pair: Go sees a []float64, and the pointer is
    // borrowed for the duration of the call.
    double dot(const double *ys, std::size_t n) const;

    // Throws std::out_of_range for an index other than 0 or 1. This is the
    // deliberate failure path: the exception's message reaches Go as the text
    // of a *cppgo.CallError.
    double at(std::size_t index) const;

    // Out of line and noexcept: the only member the direct carrier can take.
    // The generated Go signature has no error result at all.
    void scale(double k) noexcept;

    // A static member has no `this`; cppgo binds it as the package function
    // VectorDims.
    static int dims();

private:
    double x_;
    double y_;
};

// A 16-byte POD in and out, by value: the shim copies it into the call and the
// result back into Go storage.
Point midpoint(Point a, Point b);

// The BLAS-shaped free function: a scalar and two borrowed slices. The
// generated Go signature checks that x and y have the same length before the
// call, because the C++ side has a single length for both.
void saxpy(double a, const double *x, double *y, std::size_t n);

} // namespace mathx
