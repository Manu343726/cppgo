#pragma once

// bench — the C++ work every variant of the comparison performs.
//
// This header is the single source of that work. The same libbench.so is
// loaded by the cgo arm and by the cppgo arm, and the plain-Go baseline in
// internal/benchdata re-implements each function below in Go. Nothing here is
// shaped to favour a variant: the declarations differ only where a carrier
// rule in DESIGN.md makes them differ, and each of those is deliberate and
// named in the comment above it.
//
// The carrier split the generator will choose:
//
//   - add is noexcept and defined out of line, so it is called directly
//     through its mangled symbol (DESIGN.md, *Direct*);
//   - add_thunk is the same arithmetic without noexcept, so it goes through
//     the shim thunk — the price of a thunk on identical work;
//   - midpoint, blend, describe, doubled, ratio, parse and fold are thunks for
//     the reasons stated above each;
//   - Counter's constructor, destructor and noexcept members are the object
//     lifecycle: New, Close, and a method call on a live object.
//
// Nothing in this file knows that cppgo exists. The hand-written cgo shim in
// cgo/shim.cpp is a second, independent way to reach exactly these same
// symbols.

#include <cstddef>
#include <cstdint>
#include <functional>
#include <string>
#include <string_view>
#include <vector>

namespace bench {

// add is the crossing floor: two integers in, one out, no work worth pricing
// beside it. noexcept and defined out of line, so the direct carrier takes it.
std::int64_t add(std::int64_t a, std::int64_t b) noexcept;

// add_thunk is add's body with the noexcept removed. It is the only difference
// between the two, and it moves the declaration to the shim thunk, which is
// the measurement: what one C++ call frame plus the status out-parameter costs
// on work that is otherwise identical.
std::int64_t add_thunk(std::int64_t a, std::int64_t b);

// Point is 16 bytes and register-class: the Go value is the C++ layout.
struct Point {
    double x;
    double y;
};

// Sample is 24 bytes: a result above the 16-byte register class, so it comes
// back through memory.
struct Sample {
    double a;
    double b;
    double c;
};

// midpoint takes a 16-byte POD by value and returns one, which the shim has to
// copy in and out. It is not noexcept, which is what puts it on the thunk.
Point midpoint(Point a, Point b);

// blend takes two 24-byte PODs by value and returns one. It is noexcept, so the
// call cannot fail, but the result is larger than the register class and is
// written through storage the caller owns.
Sample blend(Sample a, Sample b, double t) noexcept;

// describe builds a std::string in C++ from a borrowed string_view and a
// double: the standard-library input and output case. The shim constructs the
// std::string and copies the result out.
std::string describe(std::string_view prefix, double value);

// doubled takes a const std::vector<double>& and returns a new vector. The shim
// constructs the input vector from the borrowed elements and copies the result
// back into Go storage.
std::vector<double> doubled(const std::vector<double> &values);

// ratio throws std::domain_error when b is zero. The shim catches it and the
// Go binding returns the message as an error; the success path is a thunk that
// never throws.
std::int32_t ratio(std::int32_t a, std::int32_t b);

// parse reports failure as a 32-bit status code, the convention `cppgo gen
// -status` maps to a Go error (DESIGN.md, *Errors*): 0 for a text with no
// leading '!', 3 for one that has it, 2 for the empty string.
std::int32_t parse(const std::string &text);

// fold calls the callback once. It is the C++ -> Go round trip: the binding is
// reentrant, the M is inside a foreign call, and the Go function runs on the
// runtime's callback path (DESIGN.md, *The callback-capable call*).
std::int32_t fold(const std::function<std::int32_t(std::int32_t, std::int32_t)> &fn,
                  std::int32_t a, std::int32_t b);

// Counter is the opaque object: the Go side owns a handle, C++ owns the
// storage, and Close destroys it through the destructor.
class Counter {
public:
    explicit Counter(std::int64_t start);

    ~Counter();

    // noexcept and out of line: the direct carrier with an implicit this.
    std::int64_t get() const noexcept;

    void bump(std::int64_t by) noexcept;

private:
    std::int64_t value_;
};

} // namespace bench
