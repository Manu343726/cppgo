#include "std.hpp"

namespace stdx {

std::string describe(const std::string &prefix, std::string_view suffix) {
    std::string out(prefix);
    out += ":";
    out.append(suffix.data(), suffix.size());
    return out;
}

std::size_t text_length(const std::string &value) { return value.size(); }

std::string_view literal_view() { return std::string_view("view"); }

std::vector<double> doubled(const std::vector<double> &values) {
    std::vector<double> out;
    out.reserve(values.size());
    for (double value : values) {
        out.push_back(value * 2.0);
    }
    return out;
}

double sum(std::span<const double> values) {
    double total = 0.0;
    for (double value : values) {
        total += value;
    }
    return total;
}

void scale_all(std::span<double> values, double factor) {
    for (double &value : values) {
        value *= factor;
    }
}

std::array<double, 3> triple(std::array<double, 3> values) {
    return {values[0] * 3.0, values[1] * 3.0, values[2] * 3.0};
}

std::optional<double> maybe(bool present, double value) {
    if (!present) {
        return std::nullopt;
    }
    return value;
}

double unwrap(std::optional<double> value, double fallback) {
    return value ? *value : fallback;
}

std::vector<std::int32_t> nothing() { return {}; }

} // namespace stdx
