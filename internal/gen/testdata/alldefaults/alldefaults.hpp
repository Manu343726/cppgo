#pragma once

// A declaration whose parameters all carry default arguments. Every prefix is a
// binding of its own, and the zero-argument one would take the name of the full
// signature: the contract drops it and reports the omission.
namespace alldefaults {

int sum(int a = 1, int b = 2) noexcept;

} // namespace alldefaults
