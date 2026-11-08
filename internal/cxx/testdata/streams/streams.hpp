#pragma once

// Stream and plain shift operators, for the operator naming rule. A fixture of
// its own because <ostream>/<istream> make every parse noticeably slower.
#include <istream>
#include <ostream>

namespace streams {

struct Printable {
    int value;
};

std::ostream &operator<<(std::ostream &out, const Printable &value);
std::istream &operator>>(std::istream &in, Printable &value);

struct Shifter {
    int value;
    int operator<<(int shift) const;
};

} // namespace streams
