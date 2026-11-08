#pragma once
#include <vector>
namespace err {
template <typename T>
struct Bag {
    std::vector<T> all() const { return {}; }
};
}
