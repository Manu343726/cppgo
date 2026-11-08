#pragma once

// The doc-comment fixture: the forms a C++ header writes and the Go doc each
// one becomes.
namespace docs {

/** A point in the plane.
 *
 * The origin is at (0, 0).
 */
struct Point {
    /// The horizontal coordinate.
    double x;
    /// The vertical coordinate.
    double y;
};

/** A circle.
 *
 * It has a centre and a radius.
 */
class Circle {
public:
    /// Build a circle from its radius.
    explicit Circle(double radius) noexcept;

    /** The radius.
     *
     * \returns the radius as a double
     */
    double radius() const noexcept;

    /// Move the centre.
    void move(double dx, double dy);

    /// Area of the circle.
    double area() const noexcept;
};

/** Add two numbers.
 *
 * \param a the first addend
 * \param b the second addend
 * \returns the sum
 */
double add(double a, double b) noexcept;

// A plain comment is not a doc comment: libclang attaches only /// and /** */,
// so the generator falls back to its own contract sentence.
double subtract2(double a, double b) noexcept;

// Subtract two numbers.
double subtract(double a, double b) noexcept;

} // namespace docs
