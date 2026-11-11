#pragma once

// The shape a callback-capable call cannot carry: seven INTEGER words where the
// SysV frame holds six. A -retain package is callback-capable throughout, and
// that carrier has no argument block to unpack, so the declaration is refused
// with the message naming the fix — fold the geometry into one POD argument.
//
// This header exists for that refusal and is never generated successfully.

#include <cstdint>

namespace retained {

using LogCallback = void (*)(std::int32_t level);

void set_many_callback(LogCallback fn) noexcept;

std::int32_t geometry(std::int32_t a, std::int32_t b, std::int32_t c, std::int32_t d,
                      std::int32_t e, std::int32_t f, std::int32_t g) noexcept;

} // namespace retained
