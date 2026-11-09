#pragma once

// The C-string fixture: `const char *` crosses as a Go string in both
// directions, and a non-const `char *` parameter is reported because a Go
// string is immutable and the callee's writes would not reach it.
namespace cstr {

const char *version() noexcept;
int length(const char *text) noexcept;
const char *name_of(int which) noexcept;

// The uncarryable half: a callee that writes through the pointer.
void fill(char *buffer) noexcept;

} // namespace cstr
