#pragma once
#include <cstdint>
#include <string_view>
namespace err {
// A standard-library view next to a callback: the thunk keeps the view for the
// whole call, and a callback may grow the goroutine stack and move the bytes it
// names, so the view is refused. The copied shapes (const char *, a (pointer,
// length) pair) are carried through a pinned copy instead.
void apply(void (*fn)(std::int32_t), std::string_view name);
}
