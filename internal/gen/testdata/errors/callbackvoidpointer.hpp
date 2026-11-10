#pragma once
#include <cstdint>
namespace err {
// A void pointer beside a callback: nothing says what it names, so nothing can
// copy it into memory a callback cannot move.
void apply(void (*fn)(std::int32_t), void *context);
}
