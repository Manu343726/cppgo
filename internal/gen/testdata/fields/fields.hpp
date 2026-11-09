#pragma once

#include <cstdint>

// The field-accessor fixture: an opaque record whose public fields are the only
// way to reach its state. A scalar, an enum, a pointer, a mirrored record, a
// record the Go side cannot hold by value, and a const one: each becomes
// Field() / SetField(v), and the const one becomes Field() alone.
namespace fields {

/** A mirrored value type, held by the record below. */
struct Point {
    /// The horizontal coordinate.
    double x;
    /// The vertical coordinate.
    double y;
};

/** A record the Go side cannot hold by value: it has a destructor. */
class Payload {
public:
    Payload() noexcept;
    ~Payload();
    /// The stored value.
    std::int32_t value() const noexcept;
    /// Store a value.
    void set(std::int32_t value) noexcept;

private:
    std::int32_t value_;
};

/** A kind, held as an enum field. */
enum class Kind : std::int32_t { Plain = 0, Fancy = 1 };

/** The record: private members hide the layout, public ones are the state. */
class Settings {
public:
    Settings() noexcept;

    /// A scalar field.
    std::int32_t count = 0;

    /// An enum field.
    Kind kind = Kind::Plain;

    /// A pointer field.
    const std::int32_t *source = nullptr;

    /// A mirrored record field, reached by value.
    Point origin{0, 0};

    /// An opaque record field, reached as a view of the field.
    Payload payload;

    /// A const field: read only.
    const std::int32_t version = 2;

private:
    std::int32_t hidden_ = 0;
};

} // namespace fields
