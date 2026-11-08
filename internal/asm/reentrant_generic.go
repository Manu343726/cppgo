//go:build !amd64

package asm

import "unsafe"

// CallReentrant is unimplemented on this architecture, for the same reason Call
// is. cppgo still builds everywhere, but a foreign call panics, because this
// package has no ABI for it.
func CallReentrant(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	panic("cppgo: unsupported architecture")
}
