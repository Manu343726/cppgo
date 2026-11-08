// Package stats is a cppgo-generated binding for a C++ library of statistics
// functions, and the worked example of the free-function and slice shapes.
//
// Everything here except doc.go is generated from include/stats/stats.hpp by the
// command in zz_cppgo.gen.go's banner: the Go API, the assembly trampolines and
// the shim sources under cppgo_shim/. Nothing in this directory is written by
// hand, and `make check` proves the committed files are what the generator
// produces.
//
// The C++ side is src/stats.cpp, built by the example's own Makefile into
// build/libstats.so. The binding links that library into the shim it compiles, so
// the Go package loads libcppgo_stats.so from this directory and finds the user's
// library beside it.
//
// The header's split is the lesson: a noexcept function defined out of line is
// called directly through its mangled symbol and its Go signature carries no
// error, while a function that may throw goes through the shim, which turns the
// exception into an error. Every function takes its data as a (pointer, length)
// pair, which is what a Go slice becomes on the wire, so nothing is copied in
// either direction, output arrays included.
package stats
