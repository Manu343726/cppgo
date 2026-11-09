#pragma once

// The declaration-selection fixture: a record whose constructor overload set
// holds one declaration this build can carry and one it cannot, and a free
// function overload set with the same split. Before declaration-level selection
// the uncarryable half refused the whole class and the whole set; what the
// fixture measures is that the carryable half still binds and the class exists.
namespace sel {

class Value {
public:
    Value(int value) noexcept;
    Value(void *opaque) noexcept;
    int get() const noexcept;
};

void plain(int value) noexcept;
void plain(void *opaque) noexcept;

} // namespace sel
