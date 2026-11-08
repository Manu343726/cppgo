#include "overloads.hpp"

namespace over {

Thing::Thing() : value_(0) {}

Thing::Thing(int32_t value) : value_(value) {}

Thing::~Thing() {}

int32_t Thing::value() const { return value_; }

int32_t Thing::value(int32_t scale) const { return value_ * scale; }

void Thing::reset() { value_ = 0; }

void Thing::reset(int32_t value) { value_ = value; }

int32_t add(int32_t a, int32_t b) noexcept { return a + b; }

double add(double a, double b) noexcept { return a + b; }

} // namespace over
