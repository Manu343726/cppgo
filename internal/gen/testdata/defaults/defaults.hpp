#pragma once

#include <cstdint>
#include <string>

// The default-argument fixture: a free function, a member function and a
// constructor whose parameters carry defaults, including one whose default
// expression contains an "=" of its own.
namespace defaults {

/** Welcome a name, with a greeting that has a default. Two strings cost four
 * INTEGER words and the std::string result two more, which is the register
 * file's whole width: a third parameter would need the argument block. */
std::string greet(const std::string &name, const std::string &greeting = "hello");

/** A shape with a configurable number of sides. */
class Shape {
public:
    /// Build a shape.
    explicit Shape(int sides = 4, double scale = 1.0);

    /// Scale it.
    void scale(double factor = 2.0, bool clamp = true);

    /// How many sides.
    int sides() const noexcept;

    /// The current scale.
    double scale_value() const noexcept;

private:
    int sides_;
    double scale_;
};

/** A free function with no defaults, for the control case. */
int plain(int a, int b) noexcept;

} // namespace defaults
