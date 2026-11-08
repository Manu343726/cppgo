#pragma once

// The template parameters a generic Go API is built from: a free function
// template with a type parameter, and a class template with a type and a
// non-type parameter.
namespace params {

template <typename T>
T twice(T value) { return value * 2; }

template <typename T, int N>
struct Fixed {
    T values[N];
};

struct Holder {
    Fixed<double, 4> data;
};

template <template <typename> class C, typename T>
struct Wrap {
    C<T> inner;
};

} // namespace params
