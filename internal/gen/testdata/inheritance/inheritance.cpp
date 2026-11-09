#include "inheritance.hpp"

namespace inherit {

Shape::Shape() noexcept : id_() {}

Shape::~Shape() {}

double Shape::area() const noexcept { return 0; }

const char *Shape::name() const noexcept { return "shape"; }

Drawable::Drawable() noexcept : draws_() {}

Drawable::~Drawable() {}

std::int32_t Drawable::draws() const noexcept { return draws_; }

void Drawable::draw() noexcept { ++draws_; }

Tagged::Tagged() noexcept : tag_(7) {}

Tagged::~Tagged() {}

std::int32_t Tagged::tag() const noexcept { return tag_; }

Rect::Rect() noexcept : width_(2) {}

Rect::~Rect() {}

double Rect::area() const noexcept { return width_ * width_; }

double Rect::width() const noexcept { return width_; }

Square::Square() noexcept {}

Square::~Square() {}

double Square::area() const noexcept { return 4 * width() * width(); }

Flat::Flat() noexcept {}

std::int32_t Flat::level() const noexcept { return 1; }

Raised::Raised() noexcept {}

std::int32_t Raised::height() const noexcept { return 2; }

} // namespace inherit
