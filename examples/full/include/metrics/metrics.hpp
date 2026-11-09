#pragma once

// metrics — the C++ side of the complete example.
//
// Nothing in this header knows that cppgo exists: no annotations, no
// extern "C", no binding declarations. It is an ordinary C++17 library with a
// CMake build, and that build compiles no generated file. `cppgo gen` reads
// this header through libclang and derives the Go API from it.
//
// Every declaration below is here to show one thing the generator does, and
// the generator's per-binding report names the carrier it picked:
//
//   - the opaque class has private state, out-of-line construction and
//     destruction, noexcept accessors, methods that can throw, a static member
//     and std::string / std::vector on both sides of the call;
//   - Point is a POD passed by value: the thunk copies it into the call and the
//     result into Go storage;
//   - ratio throws, so its Go signature returns an error;
//   - validate_name reports failure as a 32-bit status code, which -status
//     turns into an error too;
//   - apply takes a C++ function pointer the Go side backs with a Go
//     function, so the call runs through the callback-capable carrier;
//   - Accumulator and twice are templates: the first `cppgo gen` writes only
//     their generic Go API, and `cppgo build -scan` adds the specializations
//     the Go program actually uses.

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace metrics {

// A POD passed and returned by value. Two doubles are 16 bytes with no
// MEMORY-class part, so the direct carrier can put it in registers both ways.
struct Point {
    double x;
    double y;
};

// An opaque class: all of its state is private, and its size and layout stay
// on the C++ side. Go holds a pointer to an object C++ constructed.
//
// The state lives behind an Impl pointer, the usual pimpl: the header carries
// no standard-library member, so a change to the implementation does not
// recompile every caller. The Go side never sees any of it either way.
class Histogram {
public:
    Histogram(std::string name, std::size_t buckets);

    // A copy constructor is reachable from Go as Clone.
    Histogram(const Histogram &other);

    ~Histogram();

    // noexcept, non-virtual and defined out of line: the direct carrier calls
    // these through their mangled symbols and their Go signatures carry no
    // error.
    std::size_t buckets() const noexcept;
    double total() const noexcept;

    // A static member becomes a package-level Go function.
    static std::int32_t version() noexcept;

    // Not noexcept: the shim wraps the call, so a throw arrives in Go as an
    // error instead of unwinding into the runtime.
    void observe(double value);
    double mean() const;

    // std::string and std::vector: the shim constructs them and copies the
    // result into Go values.
    std::string name() const;
    std::vector<double> bounds() const;
    std::vector<std::size_t> counts() const;

private:
    struct Impl;
    Impl *impl_;
};

// A POD by value, noexcept and out of line: the direct carrier in both
// directions.
Point centroid(Point a, Point b) noexcept;

// std::string in and out, with a POD by reference beside it.
std::string describe(const std::string &prefix, const Point &p);

// std::vector in and out: the slice is copied into a std::vector, scaled, and
// copied back into a fresh Go slice.
std::vector<double> scaled(const std::vector<double> &values, double factor);

// Throws std::invalid_argument on a zero denominator.
double ratio(double numerator, double denominator);

// Status convention: 0 means success, any other value is a failure. The
// generator is told about it with -status, because a 32-bit integer alone does
// not say that it is a status.
std::int32_t validate_name(const std::string &name);

// A C++ -> Go callback. The Go side hands over a Go function, the callback
// table gives it an address the C ABI can call, and the call enters C through
// the reentrant carrier.
double apply(double (*fn)(double), double value);

// A class template. The generic Go API exists after `cppgo gen`; each
// specialization the Go program type-checks is instantiated in the shim by
// `cppgo build -scan`.
template <typename T>
class Accumulator {
public:
    Accumulator() : total_() {}
    explicit Accumulator(T start) : total_(start) {}
    ~Accumulator() {}
    T total() const { return total_; }
    void add(T value) { total_ += value; }
    static T identity(T value) { return value; }

private:
    T total_;
};

// A free function template, collected the same way.
template <typename T>
T twice(T value) {
    return value * 2;
}

} // namespace metrics
