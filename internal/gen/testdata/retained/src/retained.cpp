#include "retained/retained.hpp"

namespace retained {

namespace {

LogCallback g_callback = nullptr;

} // namespace

void set_log_callback(LogCallback fn) noexcept { g_callback = fn; }

void log(std::int32_t n) noexcept {
    if (g_callback == nullptr) {
        return;
    }
    for (std::int32_t level = 0; level < n; ++level) {
        g_callback(level, level % 2 == 0 ? "even" : "odd");
    }
}

std::int32_t touch() noexcept { return 7; }

Enrollment enroll(const char *label, std::int32_t count) noexcept {
    return Enrollment{label == nullptr ? std::string() : std::string(label), count};
}

} // namespace retained
