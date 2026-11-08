//go:build !amd64

package asm

import "unsafe"

// Call is unimplemented on this architecture. cppgo still builds everywhere,
// and a dependency graph that includes it stays clean on a machine that cannot
// run it. The first attempt to call a foreign function panics.
func Call(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	panic("cppgo: unsupported architecture")
}
