#include "operators.hpp"

namespace ops {

Vec2 operator+(const Vec2 &a, const Vec2 &b) { return Vec2{a.x + b.x, a.y + b.y}; }

Vec2 operator-(const Vec2 &a, const Vec2 &b) { return Vec2{a.x - b.x, a.y - b.y}; }

bool operator==(const Vec2 &a, const Vec2 &b) { return a.x == b.x && a.y == b.y; }

Number::Number(double value) noexcept : value_(value) {}

double Number::value() const noexcept { return value_; }

Number &Number::operator+=(const Number &other) {
    value_ += other.value_;
    return *this;
}

Number &Number::operator=(const Number &other) {
    value_ = other.value_;
    return *this;
}

bool Number::operator==(const Number &other) const { return value_ == other.value_; }

bool Number::operator<(const Number &other) const { return value_ < other.value_; }

double Number::operator[](int index) const { return value_ + index; }

double Number::operator()(int row, int col) const { return value_ * row + col; }

Number::operator double() const { return value_; }

Number::operator bool() const { return value_ != 0; }

Number::operator int() const { return static_cast<int>(value_); }

Number::operator std::string() const { return std::to_string(value_); }

double operator*(const Number &number, double factor) { return number.value() * factor; }

bool operator!=(const Vec2 &a, const Vec2 &b) { return !(a == b); }

} // namespace ops
