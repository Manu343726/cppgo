#pragma once

#include <cstdint>

// The nesting fixture: a type declared inside a record, an enum declared inside
// a record, a type nested two levels deep, and the same constructs inside a
// class template specialization, which libclang exposes only through the
// pattern and the model synthesizes.
namespace nested {

/** The outer type. */
class Outer {
public:
    /** A nested value type. */
    struct Inner {
        /// The horizontal coordinate.
        std::int32_t x;
        /// The vertical coordinate.
        std::int32_t y;
    };

    /** A nested opaque type. */
    class Holder {
    public:
        /// Build a holder.
        explicit Holder(std::int32_t value) noexcept;
        /// The held value.
        std::int32_t value() const noexcept;

        /** A nested enum, two levels inside Outer. */
        enum class Level { Low = 0, High = 1 };

        /// The level this holder was built at.
        Level level() const noexcept;

    private:
        std::int32_t value_;
    };

    /** A nested enum. */
    enum class Mode { Off = 0, On = 1 };

    /// Build an inner value.
    Inner make(std::int32_t x, std::int32_t y) const noexcept;

    /// The mode of this outer instance.
    Mode mode() const noexcept;

    /// A public field, so the accessor pair is exercised on an opaque record.
    std::int32_t value = 0;
};

/** A class template whose nested types a specialization replaces. */
template <typename T>
struct Box {
    /** A nested value type that depends on the parameter. */
    struct Slot {
        T value;
    };

    /** A nested enum. */
    enum class Kind { Empty = 0, Full = 1 };

    Box() noexcept : value_(), slot_(), kind_(Kind::Empty) {}
    ~Box() {}

    /// The stored value.
    T get() const { return value_; }

    /// Store a value.
    void set(T value) noexcept {
        value_ = value;
        slot_.value = value;
        kind_ = Kind::Full;
    }

    /// The slot, as a reference: a synthesized specialization's nested type is
    /// reached through its address rather than by value.
    const Slot &slot() const noexcept { return slot_; }

    /// The kind.
    Kind kind() const noexcept { return kind_; }

private:
    T value_;
    Slot slot_;
    Kind kind_;
};

} // namespace nested
