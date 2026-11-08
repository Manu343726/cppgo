#pragma once
namespace err {
template <typename T>
struct Holder {
    template <typename U>
    U identity(U value) const { return value; }
};
}
