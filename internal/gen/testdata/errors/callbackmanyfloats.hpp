#pragma once
namespace err {
// Nine SSE arguments: one more than the callback trampoline reads.
double apply(double (*fn)(double, double, double, double, double, double, double, double, double),
             double x);
}
