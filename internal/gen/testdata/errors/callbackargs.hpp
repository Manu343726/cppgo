#pragma once
namespace err {
// Seven INTEGER arguments: one more than the callback trampoline reads.
int apply(int (*fn)(int, int, int, int, int, int, int), int a);
}
