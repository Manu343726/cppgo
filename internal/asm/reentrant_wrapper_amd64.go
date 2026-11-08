//go:build amd64

package asm

import "unsafe"

// reentrantWrapper is implemented in reentrant_amd64.s and is never called from
// Go: runtime.cgocall calls it with the C calling convention, one pointer
// argument in DI. The declaration is what vet's asmdecl checker needs in order
// to see that the assembly symbol is a function of this package, and it is also
// where the shape of that call is written down next to the address that is
// taken from it.
//
//go:noescape
func reentrantWrapper(arg unsafe.Pointer)

// reentrantWrapperPtr is implemented in reentrant_amd64.s: the address of the
// register-loading wrapper runtime.cgocall calls. It returns the address as a
// pointer because that is what cgocall takes; the value is a text address, not
// Go memory.
//
//go:noescape
func reentrantWrapperPtr() unsafe.Pointer
