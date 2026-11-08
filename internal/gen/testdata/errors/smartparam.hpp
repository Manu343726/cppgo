#pragma once
#include <memory>
namespace err {
class V {
public:
    V();
    ~V();
};
int take(std::unique_ptr<V> value);
}
