#include "status.hpp"

namespace status {

std::int32_t parse(const std::string &text) {
    if (text.empty()) {
        return 2;
    }
    return 0;
}

double ratio(double a, double b) { return b == 0.0 ? 0.0 : a / b; }

} // namespace status
