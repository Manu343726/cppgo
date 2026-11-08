#pragma once
namespace err {
struct Pod { int x; };
// A record as a callback argument: the register file carries scalars only.
int apply(void (*fn)(Pod), int a);
}
