// Package mathx is a cppgo-generated binding for a C++17 class, and the worked
// example of the header-first flow.
//
// Everything here except doc.go is generated from include/mathx/vector.hpp by the
// command in zz_cppgo.gen.go's banner: the Go API, the assembly trampolines and
// the shim sources under cppgo_shim/. Nothing in this directory is written by
// hand, and `make check` proves the committed files are what the generator
// produces.
//
// The C++ side is src/vector.cpp, built by the example's own Makefile into
// build/libmathx.so with the flags a normal C++ project would use. The binding
// links that library into the shim it compiles, so the Go package loads
// libcppgo_mathx.so from this directory and finds the user's library beside it.
//
// The tests in this package are the teaching material: they show what a caller
// writes for a constructor, a method, a free function and an error result.
package mathx
