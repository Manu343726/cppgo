#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <span>
#include <string>
#include <string_view>
#include <vector>

// The standard-library fixture: one function per type in DESIGN.md's table, as
// a parameter and as a result, so the generated thunk is exercised both ways.
namespace stdx {

std::string describe(const std::string &prefix, std::string_view suffix);

std::size_t text_length(const std::string &value);

std::string_view literal_view();

std::vector<double> doubled(const std::vector<double> &values);

double sum(std::span<const double> values);

void scale_all(std::span<double> values, double factor);

std::array<double, 3> triple(std::array<double, 3> values);

std::optional<double> maybe(bool present, double value);

double unwrap(std::optional<double> value, double fallback);

std::vector<std::int32_t> nothing();

} // namespace stdx
