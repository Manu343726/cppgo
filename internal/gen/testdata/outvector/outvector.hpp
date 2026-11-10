#pragma once

#include <vector>

// The std::vector out-parameter fixture: a non-const reference the callee
// replaces, which crosses as *[]T with a copy-back block, and a const
// reference, which is an ordinary input slice.
namespace outv {

void fill(std::vector<int> &values);
int total(const std::vector<int> &values) noexcept;

} // namespace outv
