#pragma once

#include <cstdint>
#include <string>

// The static-data fixture: a mutable static member, a const one with an
// in-class initialiser, and one whose type is a standard-library value. The Go
// API reads every one through <Type><Field>() and writes the mutable ones
// through <Type>Set<Field>.
namespace statics {

/** A counter shared by every instance. */
class Counter {
public:
    /** The shared count. */
    static std::int32_t count;

    /// The fixed limit: no setter is generated.
    static const std::int32_t limit = 100;

    /// The shared label.
    static std::string label;

    /// A const string: read through the thunk, never written.
    static const std::string name;

    /// The number of instances built, as a static function beside the data.
    static std::int32_t instances() noexcept;
};

} // namespace statics
