#pragma once

// A callback-capable call whose result is a std::string: the thunk copies the
// bytes out through two words, which the call pins for the duration.

#include <cstdint>
#include <string>

namespace payloads {

std::string describe(const char *prefix, void (*fn)(std::int32_t n));

} // namespace payloads
