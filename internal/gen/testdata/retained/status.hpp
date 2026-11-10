#pragma once

// The retained declaration as a status convention: the library reports whether
// it took the callback, and the Go binding reports that as the error beside the
// registration.

#include <cstdint>

namespace retained {

using LogCallback = void (*)(std::int32_t level);

std::int32_t set_status_callback(LogCallback fn);

} // namespace retained
