#include "enums.hpp"

namespace enums {

Axis opposite(Axis axis) noexcept {
    switch (axis) {
    case Axis::X:
        return Axis::Y;
    case Axis::Y:
        return Axis::Z;
    default:
        return Axis::X;
    }
}

int axis_count() noexcept { return 3; }

bool has_flag(Flags flags, Flags flag) noexcept {
    return (static_cast<int>(flags) & static_cast<int>(flag)) != 0;
}

Mode next_mode(Mode mode) noexcept {
    return mode == Mode::Off ? Mode::On : Mode::Off;
}

} // namespace enums
