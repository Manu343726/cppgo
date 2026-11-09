#include "defaults.hpp"

namespace defaults {

std::string greet(const std::string &name, const std::string &greeting) {
    return greeting + " " + name;
}

Shape::Shape(int sides, double scale) : sides_(sides), scale_(scale) {}

void Shape::scale(double factor, bool clamp) {
    scale_ *= factor;
    if (clamp && scale_ > 100.0) {
        scale_ = 100.0;
    }
}

int Shape::sides() const noexcept { return sides_; }

double Shape::scale_value() const noexcept { return scale_; }

int plain(int a, int b) noexcept { return a - b; }

} // namespace defaults
