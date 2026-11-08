#pragma once
namespace err {
// A float callback argument: the frame carries eight doubles, not eight floats.
double eval(float (*fn)(float), float x);
}
