#pragma once

#include <string>

// The operator fixture: the member and free operators a class declares, and the
// conversion operators. Each one becomes a Go method with the fixed name the
// contract's table gives it.
namespace ops {

/** A plane vector: a mirrored POD, so an operator can return it by value. */
struct Vec2 {
    /// The horizontal component.
    double x;
    /// The vertical component.
    double y;
};

/// Sum of two vectors.
Vec2 operator+(const Vec2 &a, const Vec2 &b);

/// Difference of two vectors.
Vec2 operator-(const Vec2 &a, const Vec2 &b);

/// Equality of two vectors.
bool operator==(const Vec2 &a, const Vec2 &b);

/** A number with an overloaded arithmetic surface. */
class Number {
public:
    /// Build a number.
    explicit Number(double value) noexcept;

    /// The value it holds.
    double value() const noexcept;

    /// Add another number in place.
    Number &operator+=(const Number &other);

    /// Assign another number.
    Number &operator=(const Number &other);

    /// Equality.
    bool operator==(const Number &other) const;

    /// Ordering.
    bool operator<(const Number &other) const;

    /// Element access.
    double operator[](int index) const;

    /// Call with two indices.
    double operator()(int row, int col) const;

    /// Convert to a double.
    operator double() const;

    /// Convert to a bool: true when the value is not zero.
    operator bool() const;

    /// Convert to an int.
    explicit operator int() const;

    /// Convert to a string.
    operator std::string() const;

private:
    double value_;
};

/// A free operator with a scalar operand.
double operator*(const Number &number, double factor);

/// Inequality of two vectors.
bool operator!=(const Vec2 &a, const Vec2 &b);

} // namespace ops
