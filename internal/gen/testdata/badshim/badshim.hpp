#pragma once

// A template whose member body is invalid for the instantiation the manifest
// names. The model can still read the signature, so the only compiler that can
// prove the thunk wrong is the one that parses the generated shim.
namespace bad {

template <typename T>
class Bad {
public:
    T get() const { return value_.missing(); }
    void set(T value) { value_ = value; }

private:
    T value_;
};

} // namespace bad
