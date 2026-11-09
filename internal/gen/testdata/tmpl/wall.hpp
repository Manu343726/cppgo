#pragma once

// The Eigen shape in miniature: a class template one of whose members is only
// valid for some specializations. The generic API skips the operator (its name
// is spelled per instantiation), so the API never calls the static-asserting
// member — and a class-level `template class` in the shim would instantiate it
// anyway and refuse a type the API can use.
namespace wall {

template <typename T>
class Strict {
public:
    Strict() : value_{} {}
    ~Strict() {}
    T get() const { return value_; }
    void set(T value) { value_ = value; }

    /// Valid only for the specializations the vectors use.
    T operator()(int index) const {
        static_assert(sizeof(T) == 1, "THIS_METHOD_IS_ONLY_FOR_VECTORS_OF_A_SPECIFIC_SIZE");
        return value_ + index;
    }

private:
    T value_;
};

} // namespace wall
