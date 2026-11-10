#pragma once

#include <span>

// The span-result fixture: a result that is a *view* of storage the Go side does
// not own. The mapping has a Go shape for a span parameter — the caller's slice —
// but as a result it would name C++ storage with no owner Go holds, so the
// declaration is reported instead of handing out an address that can dangle.
namespace spanret {

std::span<int> viewOf(int *values, int count) noexcept;

/// A declaration the same header binds, so the refusal is provably one
/// declaration and not the file.
int count() noexcept;

} // namespace spanret
