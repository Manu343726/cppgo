#pragma once
namespace err {
struct Sample { double a; };
// A record as the callback's result: the register file carries one scalar.
double apply(Sample (*fn)(double), double x);
}
