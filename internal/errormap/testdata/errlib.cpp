#include "errlib.hpp"

#include <string>
#include <system_error>

namespace ns {

std::error_code open_file(const std::string &path, std::int32_t *out_fd) {
    if (path.empty()) {
        return std::make_error_code(std::errc::invalid_argument);
    }
    if (path == "missing.txt") {
        return std::make_error_code(std::errc::no_such_file_or_directory);
    }
    *out_fd = 42;
    return {};
}

std::expected<std::int32_t, std::error_code> parse_int(const std::string &text) {
    if (text.empty()) {
        return std::unexpected(std::make_error_code(std::errc::invalid_argument));
    }
    for (char c : text) {
        if (c < '0' || c > '9') {
            return std::unexpected(std::make_error_code(std::errc::invalid_argument));
        }
    }
    return std::stoi(text);
}

std::int32_t divide(std::int32_t a, std::int32_t b) {
    if (b == 0) {
        throw std::runtime_error("division by zero");
    }
    return a / b;
}

std::int32_t connect(const std::string &host) {
    if (host != "localhost") {
        throw std::system_error(std::make_error_code(std::errc::host_unreachable),
                                "connecting to " + host);
    }
    return 1;
}

bool parse_point(const char *text, Point *out, std::int32_t *status) {
    Point p{};
    if (text == nullptr || std::sscanf(text, "%d,%d", &p.x, &p.y) != 2) {
        *status = 1; // 1 = bad syntax, in this library's own numbering
        return false;
    }
    *out = p;
    *status = 0;
    return true;
}

Result<std::int32_t> load(std::int32_t key) {
    if (key < 0) {
        return Result<std::int32_t>(std::make_error_code(std::errc::invalid_argument));
    }
    return Result<std::int32_t>(key * 2);
}

} // namespace ns
