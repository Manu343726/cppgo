//go:build !amd64

package asm

import "unsafe"

// Call is unimplemented on this architecture. cppgo still builds everywhere so
// that a dependency graph including it is not poisoned on a machine that
// cannot run it, but the first attempt to call a foreign function panics.
func Call(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	panic("cppgo: unsupported architecture")
}
