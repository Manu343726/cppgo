#pragma once

// A declaration that returns a value and takes a callback, which -retain
// refuses: the registration is the call's outcome, and a value would have to
// share it.

#include <cstdint>

namespace retained {

using LogCallback = void (*)(std::int32_t level);

std::int32_t set_valued_callback(LogCallback fn) noexcept;

} // namespace retained
