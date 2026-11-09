#include "nested.hpp"

namespace nested {

Outer::Holder::Holder(std::int32_t value) noexcept : value_(value) {}

std::int32_t Outer::Holder::value() const noexcept { return value_; }

Outer::Holder::Level Outer::Holder::level() const noexcept {
    return value_ > 0 ? Level::High : Level::Low;
}

Outer::Mode Outer::mode() const noexcept { return Mode::On; }

Outer::Inner Outer::make(std::int32_t x, std::int32_t y) const noexcept { return Inner{x, y}; }

template struct Box<int>;
template struct Box<double>;

} // namespace nested
