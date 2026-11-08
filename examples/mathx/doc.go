// Package mathx binds a small C++17 class to Go, and is the worked example of the
// header-first flow.
//
// It is generated from include/mathx/vector.hpp: the Go types and functions here
// are the C++ API as a caller sees it, with Go values and Go errors. It is
// generator output, not hand-written code.
//
// The C++ side is src/vector.cpp, built by the example's own Makefile. A caller
// needs nothing from it but the generated package: the two shared objects it loads
// are found beside the code, and the library path is the only thing a deployment
// can change.
//
// The tests show what a caller writes for a constructor, a method, an inline
// accessor, a free function and a call that can fail.
package mathx
