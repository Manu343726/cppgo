#pragma once
namespace err {
struct WithMember {
    template <typename T>
    T identity(T value) const { return value; }
};
}
