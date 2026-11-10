#pragma once

// A header whose callbacks are named by typedefs, which is how a real library
// writes them: the declaration spells the alias, and the signature lives in
// the `using`.

namespace aliases {

// A callback whose arguments are plain scalars: the alias resolves to the
// function pointer, and the pointer's own signature decides the Go function
// type.
using CountCallback = void (*)(int n);

void set_count_callback(CountCallback fn);
void count_em_up(int n);

} // namespace aliases
