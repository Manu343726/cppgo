#include "fields.hpp"

namespace fields {

Payload::Payload() noexcept : value_(0) {}

Payload::~Payload() {}

std::int32_t Payload::value() const noexcept { return value_; }

void Payload::set(std::int32_t value) noexcept { value_ = value; }

Settings::Settings() noexcept {}

} // namespace fields
