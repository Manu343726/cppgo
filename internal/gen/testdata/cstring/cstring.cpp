#include "cstring.hpp"

#include <cstring>

namespace cstr {

const char *version() noexcept { return "1.2.3"; }

int length(const char *text) noexcept {
    return text == nullptr ? -1 : static_cast<int>(std::strlen(text));
}

const char *name_of(int which) noexcept { return which == 0 ? "zero" : "other"; }

void fill(char *buffer) noexcept {
    if (buffer != nullptr) {
        buffer[0] = 'x';
    }
}

} // namespace cstr
