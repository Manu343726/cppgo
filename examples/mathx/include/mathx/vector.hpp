#pragma once

#include <cstddef>

namespace mathx {

// Vector is the whole direct-ABI story in one small class.
//
// Every member cppgo binds is DECLARED here and DEFINED OUT OF LINE in
// src/vector.cpp. That split is not style, it is the requirement: a member
// defined inside the class body is implicitly inline, so the compiler emits no
// symbol for it and dlsym has nothing to find. `norm_squared` below is the
// counter-example, left in place so the failure can be reproduced.
class Vector {
public:
    // A constructor and a destructor are resolved to C1/D1 (the
    // complete-object entries) by libclang, never by a mangled name guessed in
    // Go. See the report `cppgo gen` prints.
    Vector(double x, double y);
    ~Vector();

    // const members carry a K in the mangling (_ZNK5mathx6Vector1xEv) and take
    // `this` as their first integer argument, exactly like a non-const one.
    double x() const;
    double y() const;
    double norm() const;

    // A method taking a slice: on the wire that is (pointer, length) right
    // after `this` — RSI and RDX here.
    double dot(const double *ys, std::size_t n) const;

    // A mutator: the same `this`, no result.
    void scale(double k);

    // A static member function has no `this`. It is bound with
    // //cppgo:function, not //cppgo:method.
    static int dims();

    // Defined inside the class definition, therefore implicitly inline: the
    // library exports no symbol for it and cppgo refuses to bind it. Run
    // `make inline-demo` to see the build error it produces.
    double norm_squared() const { return x_ * x_ + y_ * y_; }

private:
    // The layout is private and unknown to Go. cppgo allocates sizeof(Vector)
    // bytes (learned from libclang, not hard-coded) and hands the address to
    // the constructor; everything else lives on the C++ side of the pointer.
    double x_;
    double y_;
};

} // namespace mathx
