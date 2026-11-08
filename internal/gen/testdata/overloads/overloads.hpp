#pragma once

#include <cstdint>

// The overload fixture: constructors, methods and free functions that map to a
// shared Go base name and therefore carry a parameter-type suffix.
namespace over {

class Thing {
public:
    Thing();
    Thing(int32_t value);
    ~Thing();

    int32_t value() const;
    int32_t value(int32_t scale) const;

    void reset();
    void reset(int32_t value);

private:
    int32_t value_;
};

int32_t add(int32_t a, int32_t b) noexcept;
double add(double a, double b) noexcept;

} // namespace over
