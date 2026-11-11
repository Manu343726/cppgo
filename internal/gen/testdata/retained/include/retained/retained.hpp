#pragma once

// A small logging library with the one shape a retained callback is about: the
// callee stores the Go function and calls it later, during calls of its own.
//
// Nothing here knows that cppgo exists: no annotation says the pointer is
// stored, which is why the generator has to be told with -retain.

#include <cstddef>
#include <cstdint>

#include "shapes.hpp"

namespace retained {

// The level is spdlog's ordering truncated to the five this library writes.
using LogCallback = void (*)(std::int32_t level, const char *message);

// Stores fn, replacing whatever was stored. A null pointer turns logging off.
// The library makes no thread of its own: a record is delivered from inside a
// call made from Go, on the goroutine that made it.
void set_log_callback(LogCallback fn) noexcept;

// Writes n records through the stored callback, one per call, and does nothing
// when no callback is stored.
void log(std::int32_t n) noexcept;

// A call that never delivers a record, so a test can tell "no callback" from
// "a callback that was not called".
std::int32_t touch() noexcept;

} // namespace retained
