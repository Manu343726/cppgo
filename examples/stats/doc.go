// Package stats binds a C++ library of statistics functions to Go, and is the
// worked example of the free-function and slice shapes.
//
// It is generated from include/stats/stats.hpp: the Go functions here are the C++
// API as a caller sees it, taking Go slices and returning Go values and errors. It
// is generator output, not hand-written code.
//
// The C++ side is src/stats.cpp, built by the example's own Makefile. A caller
// needs nothing from it but the generated package: the two shared objects it loads
// are found beside the code, and the library path is the only thing a deployment
// can change.
//
// The header's split is the lesson. A function that cannot fail returns a value
// and no error; a function that can fails with an error carrying the text the C++
// side produced. Data crosses as a slice in both directions, including an output
// array the function writes into.
package stats
