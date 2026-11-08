#include "cell.hpp"

namespace tmpl {

template <typename T>
Cell<T>::Cell(T value) : value_(value) {}

template <typename T>
Cell<T>::~Cell() {}

template <typename T>
T Cell<T>::get() const { return value_; }

template <typename T>
void Cell<T>::set(T value) { value_ = value; }

template class Cell<int>;
template class Cell<double>;

} // namespace tmpl
