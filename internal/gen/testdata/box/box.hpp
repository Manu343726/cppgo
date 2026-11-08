#pragma once

// A header-only class template and a free function template: the two-phase
// fixture. Every member is defined inline, so the shim is the whole binary and
// the instantiation lines are what make the specializations exist.
namespace box {

template <typename T>
class Box {
public:
    Box() : value_() {}
    Box(T value) : value_(value) {}
    Box(const Box &other) = delete;
    ~Box() {}
    T get() const { return value_; }
    void set(T value) { value_ = value; }
    T add(T other) const { return value_ + other; }
    T *echo(T *value) const { return value; }
    const T *peek(const T *value) const { return value; }
    void consume(const T &value) { value_ = value; }
    static T identity(T value) { return value; }

private:
    T secret() const { return value_; }
    T value_;
};

// A template with no declared destructor: Close releases the storage alone.
template <typename T>
class Plain {
public:
    Plain(T value) : value_(value) {}
    T value() const { return value_; }

private:
    T value_;
};

// The declarations the generic API skips because C++ makes them unusable.
template <typename T>
struct Deleted {
    Deleted() = delete;
    ~Deleted() = delete;
    T value() const = delete;
    static T shared() = delete;

private:
    T value_;
};

// A pure virtual member has no address to call.
template <typename T>
struct Abstract {
    virtual ~Abstract() {}
    virtual T get() const = 0;
};

// A fixed-dimension form: Go has no value parameters, so R and C are phantom
// types that name the C++ integers the shim instantiates.
template <typename T, int R, int C>
class Matrix {
public:
    Matrix(T fill) : fill_(fill) {}
    T get() const { return fill_; }
    static int rows() { return R; }

private:
    T fill_;
};

template <typename T>
T twice(T value) { return value * 2; }

} // namespace box
