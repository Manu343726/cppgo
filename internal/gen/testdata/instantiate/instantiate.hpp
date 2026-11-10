#pragma once

// The manifest-instantiation fixture: a plain record with a member function
// template, and a free function template. Both have no Go spelling on their own
// (Go has no type parameters on methods, and a function template does not exist
// until it is called), so -instantiate names them.
#include <vector>

namespace ins {

class Box {
public:
    Box() noexcept;
    int value() const noexcept;
    template <typename T>
    T get() const noexcept;
};

template <typename T>
T twice(T value) noexcept;

/// A free function template whose parameter is a container of containers: the
/// substituted spelling is where a standard-library type meets the
/// instantiation, and the recursive mapping has to see it.
template <typename T>
int totalNested(const std::vector<std::vector<T>> &values) noexcept;

} // namespace ins
