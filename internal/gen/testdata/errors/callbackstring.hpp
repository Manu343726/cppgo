#pragma once
#include <string>
namespace err {
// A std::string as a callback argument: the frame carries no C++ object.
void apply(void (*fn)(std::string), std::string value);
}
