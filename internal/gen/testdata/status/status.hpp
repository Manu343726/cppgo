#pragma once

#include <cstdint>
#include <string>

// A library that reports failures as a 32-bit status code and a message beside
// it, which is the convention -status exists for: the Go signature returns the
// error and never the integer.
namespace status {

// Returns 0 on success and a non-zero code on failure.
std::int32_t parse(const std::string &text);

// A function with a value result is not a status convention, and -status on it
// is a generation error naming the declaration.
double ratio(double a, double b);

} // namespace status
