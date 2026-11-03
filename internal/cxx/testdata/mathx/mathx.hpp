#pragma once

#include <cstddef>

namespace mathx {

enum class Axis : unsigned char { X = 0, Y = 1 };

// Vector is the shape cppgo's direct ABI mode supports: a constructor and every
// method defined OUT OF LINE in mathx.cpp, so each one emits a dynamic symbol.
class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    double norm() const;
    double dot(const double *ys, std::size_t n) const;
    Vector scaled(double k) const;

    // Defined inside the class definition, therefore implicitly inline and
    // invisible to dlsym: the generator must refuse to bind these.
    double x() const { return x_; }
    double y() const { return y_; }

    // Virtual dispatch goes through the vtable: needs the shim mode.
    virtual double scale() const;

    static int dims();
    static const char *name();

    // A pointer to member is neither an integer nor a pointer at the ABI level:
    // libclang reports MemberPointer and this package classifies it "other".
    static int pick(int Vector::*member);

private:
    double x_;
    double y_;
};

// A class whose constructor is inline too: the other shape the generator has to
// refuse.
class Point {
public:
    explicit Point(int value) : value_(value) {}

    int value() const { return value_; }

private:
    int value_;
};

// A polymorphic class: virtual destructor, a pure virtual with no body at all,
// and one ordinary out-of-line member.
class Shape {
public:
    virtual ~Shape();
    virtual double area() const = 0;
    int id() const;

protected:
    Shape(int id);

private:
    int id_;
};

// Tiny is small enough to ride in a register, but it declares a destructor, so
// the Itanium ABI returns it indirectly anyway: the caller must pass a hidden
// destination pointer. libclang has no "trivial for the purposes of calls"
// query, so this is the case that proves the structural rule in
// indirectResult.
struct Tiny {
    int value;
    ~Tiny();
};

// Pair, by contrast, is a plain aggregate: 16 bytes, trivially copyable, and it
// comes back in XMM0:XMM1 with no hidden pointer.
struct Pair {
    double a;
    double b;
};

// A deep hierarchy: five bases before the member that makes it non-trivial.
struct Level6 {
    int value;
    ~Level6();
};
struct Level5 : Level6 {};
struct Level4 : Level5 {};
struct Level3 : Level4 {};
struct Level2 : Level3 {};
struct Level1 : Level2 {};

// Small records that the ABI returns through a hidden pointer for reasons that
// have nothing to do with their size: each one is non-trivial for the purposes
// of calls in a different way. All of them are two eightbytes or less.
struct SmallVirtual {
    int value;
    virtual void touch();
};
struct SmallCopy {
    int value;
    SmallCopy();
    SmallCopy(const SmallCopy &other);
};
struct SmallMove {
    int value;
    SmallMove();
    SmallMove(SmallMove &&other);
};
struct VirtualBase {
    int value;
};
struct SmallVirtualBase : virtual VirtualBase {};
struct SmallMember {
    Tiny inner;
};

Vector make(double x, double y);
Tiny make_tiny(int value);
Level1 make_deep();
SmallVirtual make_small_virtual(int value);
SmallCopy make_small_copy(int value);
SmallMove make_small_move(int value);
SmallVirtualBase make_small_virtual_base(int value);
SmallMember make_small_member(int value);
Pair make_pair(double a, double b);
bool is_axis(Axis axis, bool strict);
int add(int a, int b);
double add(double a, double b);

// Internal linkage: a definition out of line, but no exported symbol.
static int internal_counter();

namespace {
void hidden_helper();
}

} // namespace mathx
