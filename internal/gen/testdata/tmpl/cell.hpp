#pragma once

// A class template and the explicit instantiations the manifest names. The
// instantiation sees the pattern's members through the model's substitution,
// because libclang exposes no cursor for the instantiated definition.
namespace tmpl {

template <typename T>
class Cell {
public:
    Cell(T value);
    ~Cell();
    T get() const;
    void set(T value);

private:
    T value_;
};

} // namespace tmpl
