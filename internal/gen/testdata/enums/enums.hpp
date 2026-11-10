#pragma once

#include <cstdint>

// The enum fixture: a scoped enum, an unscoped one, an explicit underlying type,
// negative and non-contiguous values, an unsigned underlying type, and an enum
// used as a parameter and as a result.
namespace enums {

/** Axis is the coordinate axis of a point. */
enum class Axis : int {
    /** The x axis. */
    X = 0,
    /** The y axis. */
    Y = 1,
    /** The z axis. */
    Z = 2,
};

/// Flags are bit flags with no fixed underlying type.
enum Flags {
    NoFlags = 0,
    Read = 1,
    Write = 2,
    Execute = 7,
};

/// Mode fixes its underlying type to a byte.
enum class Mode : std::uint8_t {
    Off = 0,
    On = 200,
    Maximum = 255,
};

/// Border has two spellings of one value: exportName folds the underscore away,
/// so REFLECT_101 and REFLECT101 are one Go constant, and the report says so.
enum Border {
    BORDER_REPLICATE = 1,
    BORDER_REFLECT_101 = 4,
    BORDER_REFLECT101 = 4,
};

/// Offset has negative and non-contiguous enumerators.
enum Offset : std::int16_t {
    Behind = -3,
    Origin = 0,
    Far = 900,
};

/// Color_t tests the C-style typedef spelling.
typedef enum {
    Red = 10,
    Green = 20,
} Color_t;

/// The opposite axis.
Axis opposite(Axis axis) noexcept;

/// The number of axes.
int axis_count() noexcept;

/// Whether a flag combination is set.
bool has_flag(Flags flags, Flags flag) noexcept;

/// A mode with a wider result type.
Mode next_mode(Mode mode) noexcept;

/// The opposite axis, computed in the header: a thunk carries the call, which
/// is where the enum is cast back to its C++ type.
inline Axis inline_opposite(Axis axis) noexcept {
    return axis == Axis::X ? Axis::Y : Axis::X;
}

/// A flag test in the header, so both parameters are cast in the thunk.
inline bool inline_has_flag(Flags flags, Flags flag) noexcept {
    return (static_cast<int>(flags) & static_cast<int>(flag)) != 0;
}

/// An inline mode transition, so an enum result is cast in the thunk.
inline Mode inline_next_mode(Mode mode) noexcept {
    return mode == Mode::Off ? Mode::On : Mode::Off;
}

} // namespace enums
