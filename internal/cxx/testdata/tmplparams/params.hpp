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

// A plain record with a member function template: a member template of a
// non-template class is what InstantiateMember takes.
struct Holder2 {
    template <typename U>
    U echo(U value) const;
    template <typename U>
    static U make(U value);
};

template <template <typename> class C, typename T>
struct Wrap {
    C<T> inner;
};

} // namespace params

namespace params {

// A pattern that derives from another: a specialization's layout has to include
// the base's fields, and its size has to agree with them.
template <typename T>
struct Base {
    T value;
};

template <typename T>
struct Derived : Base<T> {
    T extra;
};

// A pattern whose data lives in a base of the pattern.
template <typename T>
struct Ref {
    Ref();
    Ref(const Ref<T> &other);
    T get() const;
    template <typename U>
    U convert(U value) const;
    T value;
};

// A pattern whose field's type has no definition: the layout cannot be
// computed, and the record is still returned so the caller can report it.
template <typename T>
struct Missing;

template <typename T>
struct Incomplete {
    Missing<T> field;
};

// A pattern with a virtual base: the offsets are resolved through the vtable,
// so the layout is refused rather than guessed.
template <typename T>
struct VirtualBase {
    T value;
};

template <typename T>
struct VirtualDerived : virtual VirtualBase<T> {
    T extra;
};

} // namespace params
