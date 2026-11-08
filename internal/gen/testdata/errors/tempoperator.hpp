#pragma once
namespace err {
template <typename T>
struct Num {
    Num operator+(const Num &other) const;
private:
    T value_;
};
}
