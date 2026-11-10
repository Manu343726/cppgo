#pragma once

// A constructor whose last argument is a float: the trampoline has an SSE slot
// as its final argument and a 32-bit result, and the frame the generator
// computes has to be the one go vet's asmdecl check computes.
namespace floatargs {

class Measure {
public:
    Measure(int index, int label, float weight) noexcept;
    float weight() const noexcept;
};

float scale(int count, float factor) noexcept;

} // namespace floatargs
