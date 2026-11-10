#pragma once

// The record-specialization fixture: a class template with an array field (the
// shape a synthesized layout has to size) and a record that holds an
// instantiation of it.
namespace params {

template <typename T, int N>
struct Fixed {
    Fixed() noexcept;
    T values[N];
    T get(int index) const noexcept;
};

struct Holder {
    Fixed<double, 4> data;
};

} // namespace params
