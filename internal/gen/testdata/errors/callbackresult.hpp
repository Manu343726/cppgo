#pragma once
#include <cstdint>
#include <functional>
namespace err {
// A std::function result: a callable Go cannot receive from C++ yet.
std::function<std::int32_t(std::int32_t)> pick(std::int32_t which);
}
