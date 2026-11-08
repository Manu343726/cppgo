//go:build !amd64

package asm

import "unsafe"

// CallReentrant is unimplemented on this architecture, for the same reason Call
// is: the package still compiles, but a foreign call panics instead of entering
// an ABI this package has not been written for.
func CallReentrant(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	panic("cppgo: unsupported architecture")
}
