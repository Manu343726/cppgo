// Package cgotest holds cppgo's cgo-based comparison benchmark.
//
// It exists because a package that uses cgo cannot contain assembly files, and
// the point of the comparison is to put the cppgo trampoline next to the cgo
// call path in the *same* binary. That way the two numbers come from the same
// clock, the same governor and the same cache state.
//
// None of this is part of the library. The cppgo package itself never uses
// cgo, and this package compiles to no code when CGO_ENABLED=0 (the benchmark
// is behind a cgo build tag, so only this doc file remains).
package cgotest
