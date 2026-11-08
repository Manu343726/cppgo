// The user's library, as a user writes it: ordinary C++17, compiled by their
// own build, with no cppgo-generated code in it. This header is what cppgo
// would be pointed at.
#pragma once

#include <cstdint>
#include <functional>
#include <stdexcept>
#include <string>
#include <vector>

namespace user {

struct Point {
    double x;
    double y;
};

// A virtual interface with an out-of-line destructor and one out-of-line
// method: real dispatch is what the shim has to reach.
class Shape {
public:
    virtual ~Shape();
    virtual double area() const = 0;
    virtual const char *name() const;
};

class Circle : public Shape {
public:
    explicit Circle(double r);
    ~Circle() override;
    double area() const override;
    const char *name() const override;
    double radius() const;

private:
    double r_;
};

// A second base class, so that the Circle subobject sits at a non-zero offset
// and a cast to the first base is not the identity.
class Serializable {
public:
    explicit Serializable(std::int32_t id);
    virtual ~Serializable();
    virtual std::int32_t serial() const;

private:
    std::int32_t id_;
};

class Disk : public Serializable, public Circle {
public:
    Disk(std::int32_t id, double r);
    ~Disk() override;
    std::int32_t serial() const override;

private:
    std::int32_t tag_;
};

// Header-only and never called from the library's own translation unit: no
// symbol with this name exists in the library's .dynsym, and no Go trampoline
// can conjure one. A shim that includes this header compiles its body.
inline double twice(double v) { return 2 * v; }

// A non-POD value: user-declared copy constructor and destructor, both
// out of line.
class Tiny {
public:
    explicit Tiny(std::int32_t value);
    Tiny(const Tiny &other);
    ~Tiny();
    std::int32_t value() const;

private:
    std::int32_t value_;
};

// std:: types on both sides of the call.
std::string greet(const std::string &prefix, const std::string &name);
std::vector<double> doubled(const std::vector<double> &values);
double sum(const std::vector<double> &values);

// A function that throws.
std::int32_t checked_div(std::int32_t a, std::int32_t b);

// How many Disks have been destroyed: the counter that proves a delete through
// a Shape* reached the derived destructor.
std::int32_t disk_destroys();

// A header-only template: the instantiation only exists where someone asks for
// it, which is the shim.
template <typename T> T maxof(T a, T b) { return a > b ? a : b; }

// A function whose parameter is a std::function: the callable can only be
// constructed by C++.
std::int32_t apply(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                   std::int32_t a, std::int32_t b);

} // namespace user
