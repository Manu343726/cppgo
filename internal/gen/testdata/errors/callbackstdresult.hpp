#pragma once
#include <cstdint>
#include <string>
namespace err {
// A callback binding that copies a std::string out: the copy leaves through
// locals a callback could move when it grows the stack, so it is refused.
std::string apply(std::int32_t (*fn)(std::int32_t), std::int32_t a);
}
