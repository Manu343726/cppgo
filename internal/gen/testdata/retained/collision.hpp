#pragma once

// A header whose own type is named CallbackRegistration, which is the name the
// generated package reserves for a retained callback's registration.

#include <cstdint>

namespace retained {

using LogCallback = void (*)(std::int32_t level);

struct CallbackRegistration {
    std::int32_t level;
};

void set_log_callback(LogCallback fn) noexcept;

} // namespace retained
