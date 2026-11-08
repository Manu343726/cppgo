#pragma once
#include <memory>
namespace err {
class V {
public:
    V();
    ~V();
};
std::unique_ptr<V> make();
}
