#pragma once
#include <cstdint>
namespace err {
// A pointer into Go memory next to a callback: the callback may grow the
// goroutine stack and move it, so the pointer is refused.
void apply(void (*fn)(std::int32_t), const char *name);
}
