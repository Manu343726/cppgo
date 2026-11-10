#pragma once
#include <cstdint>
#include <vector>
namespace err {
// A callback binding that copies a std::vector out: the copy leaves through
// words of the goroutine's frame, which a callback may move when it grows the
// stack, so the result is refused. A std::string result is carried, through a
// pinned heap cell.
std::vector<std::int32_t> apply(std::int32_t (*fn)(std::int32_t), std::int32_t a);
}
