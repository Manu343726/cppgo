// The user's library for the error-mapping prototype: one function per failure
// convention the contract has to turn into a Go error.
//
// Ordinary C++23, compiled by the user's own build, with no cppgo-generated code
// in it. The C++23 requirement comes from std::expected, which is the point of
// one of the rows.
#pragma once

#include <cstdint>
#include <expected>
#include <string>
#include <system_error>

namespace ns {

struct Point {
    std::int32_t x;
    std::int32_t y;
};

// std::error_code: zero is success, non-zero carries a code and a category.
std::error_code open_file(const std::string &path, std::int32_t *out_fd);

// std::expected<T, std::error_code> (C++23).
std::expected<std::int32_t, std::error_code> parse_int(const std::string &text);

// Throws std::runtime_error.
std::int32_t divide(std::int32_t a, std::int32_t b);

// Throws std::system_error: a code, a category and a message.
std::int32_t connect(const std::string &host);

// bool + out parameters: the value and the failure are parameters, and nothing
// in the signature says which is which.
bool parse_point(const char *text, Point *out, std::int32_t *status);

// A project Result<T> with the inferable shape: has_value/value/error.
template <typename T> class Result {
public:
    Result(T value) : value_(value), ok_(true) {}
    Result(std::error_code ec) : ec_(ec), ok_(false) {}

    bool has_value() const { return ok_; }
    T value() const { return value_; }
    std::error_code error() const { return ec_; }

private:
    T value_{};
    std::error_code ec_{};
    bool ok_;
};

Result<std::int32_t> load(std::int32_t key);

} // namespace ns
