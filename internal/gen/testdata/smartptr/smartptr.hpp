#pragma once

#include <memory>

// The smart-pointer result fixture: the shape every OpenCV factory returns.
// The Go value is the object the pointer owns, and the control block is one
// holder the shim allocates and the Go side releases once.
namespace smart {

class Widget {
public:
    Widget() noexcept;
    ~Widget();
    int value() const noexcept;
};

std::shared_ptr<Widget> makeShared(int value);
std::unique_ptr<Widget> makeUnique(int value);
std::shared_ptr<Widget> emptyShared() noexcept;

} // namespace smart
