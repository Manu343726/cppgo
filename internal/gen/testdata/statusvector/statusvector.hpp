#pragma once

#include <vector>

// The status fixture: a callee whose own return value is the status code, and
// which also writes into a standard-library out-parameter. The copy-back has to
// run after the call, so the thunk keeps the call in a branch.
namespace statv {

/// Appends one value per input and returns 0; a non-zero value is the failure.
int fillStatus(std::vector<int> &values) noexcept;

} // namespace statv
