#pragma once

// A header whose callbacks carry the callee's memory: a NUL-terminated string,
// a counted text buffer, a counted array of integers, and a pointer to a
// record. The memory belongs to the C++ side in every case, which is what makes
// it safe to hand to Go while the callback runs.

#include <cstddef>
#include <cstdint>

namespace payloads {

struct Point {
    double x;
    double y;
};

using LogCallback = void (*)(std::int32_t level, const char *message);

void set_log_callback(LogCallback fn);

// The counted forms: two C argument words folded into one Go value.
void set_text(void (*fn)(const char *text, std::size_t n));

void set_bytes(void (*fn)(const std::uint8_t *data, std::size_t n));

void set_points(void (*fn)(const Point *points, std::size_t n));

// A pointer without a count, and an untyped one.
void set_point(void (*fn)(const Point *point));

void set_untyped(void (*fn)(void *context));

// A callback that returns a value beside its payload.
std::int32_t set_level(void (*fn)(std::int32_t level, const char *message));

// The Go pointer side: a slice, a writable record and a text prefix, each
// handed to a call that may run a callback. The generated binding copies them
// into pinned heap objects, so what the callee reads cannot move when the
// callback grows the goroutine's stack.
std::int32_t encode(const std::uint8_t *data, std::size_t n, void (*fn)(std::int32_t n));

void fill(std::uint8_t *data, std::size_t n, void (*fn)(std::int32_t n));

void shift(Point *point, void (*fn)(std::int32_t value));

const char *label(const char *prefix, void (*fn)(std::int32_t n));

} // namespace payloads
