#pragma once
namespace err {
class Value {
public:
    Value();
    ~Value();
    int get() const;
private:
    int v_;
};
int consume(Value value);
}
