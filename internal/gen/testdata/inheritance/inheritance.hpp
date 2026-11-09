#pragma once

#include <cstdint>

// The inheritance fixture: single inheritance, multiple inheritance with a
// non-zero base offset, a virtual base whose offset is a vtable lookup, a
// virtual method overridden by the derived type, and a base that is not
// polymorphic, which is what a dynamic_cast needs.
namespace inherit {

/** The root of the hierarchy, polymorphic. */
class Shape {
public:
    Shape() noexcept;
    virtual ~Shape();

    /// The area. Virtual, so a Shape reached as a Base dispatches here.
    virtual double area() const noexcept;

    /// The name every shape shares.
    const char *name() const noexcept;

    /// A value method that is not noexcept: a copy of it needs the status and
    /// the out-pointer a direct call never had.
    double weight() const;

    /// A public field of the base.
    std::int32_t id = 0;

private:
    std::int32_t id_;
};

/** A second base with its own state: the derived type holds it at an offset. */
class Drawable {
public:
    Drawable() noexcept;
    ~Drawable();

    /// How many times it was drawn.
    std::int32_t draws() const noexcept;

    /// Draw once.
    void draw() noexcept;

private:
    std::int32_t draws_;
};

/** A virtual base, whose subobject offset is resolved through the vtable. */
class Tagged {
public:
    Tagged() noexcept;
    virtual ~Tagged();

    /// The tag.
    std::int32_t tag() const noexcept;

private:
    std::int32_t tag_;
};

/** A rectangle: Shape plus Drawable plus a virtual Tagged. */
class Rect : public Shape, public Drawable, public virtual Tagged {
public:
    Rect() noexcept;
    ~Rect() override;

    /// The overridden area.
    double area() const noexcept override;

    /// Its own method.
    double width() const noexcept;

    /// A reference to itself through an out-of-line noexcept body: the direct
    /// carrier returns the view's address in the result register.
    Rect &identity() noexcept;

private:
    double width_;
};

/** A rectangle with a twist: the derived type of Rect. */
class Square : public Rect {
public:
    Square() noexcept;
    ~Square() override;

    /// The overridden area.
    double area() const noexcept override;
};

/** A shape that is not polymorphic: no vtable, so no dynamic_cast. */
class Flat {
public:
    Flat() noexcept;
    /// A method.
    std::int32_t level() const noexcept;
};

/** The derived type of a non-polymorphic base. */
class Raised : public Flat {
public:
    Raised() noexcept;
    /// Its own method.
    std::int32_t height() const noexcept;
};

} // namespace inherit
