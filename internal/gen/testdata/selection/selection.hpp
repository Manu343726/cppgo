#pragma once

// The declaration-selection fixture: a record whose constructor overload set
// holds one declaration this build can carry and one it cannot, and a free
// function overload set with the same split. Before declaration-level selection
// the uncarryable half refused the whole class and the whole set; what the
// fixture measures is that the carryable half still binds and the class exists.
//
// The uncarryable parameter is an incomplete record passed by value: the type
// is declared, so the declaration parses, and it has no definition, so the
// model cannot size the copy the call would make.
namespace sel {

class Hidden;

class Value {
public:
    Value(int value) noexcept;
    Value(Hidden opaque) noexcept;
    int get() const noexcept;
};

void plain(int value) noexcept;
void plain(Hidden opaque) noexcept;

} // namespace sel
