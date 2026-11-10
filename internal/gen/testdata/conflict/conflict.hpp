#pragma once

// The enum-collision fixture: two enumerators whose names fold to one Go
// identifier and whose values differ. They are two constants, not one, so the
// collision cannot be resolved the way an alias is.
namespace conflict {

enum Broken {
    UnderValue = 1,
    Under_Value = 2,
};

} // namespace conflict
