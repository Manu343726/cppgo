//go:build amd64

package asm

import "unsafe"

// reentrantWrapperPtr is implemented in reentrant_amd64.s: the address of the
// register-loading wrapper runtime.cgocall calls. It returns the address as a
// pointer because that is what cgocall takes; the value is a text address, not
// Go memory.
//
//go:noescape
func reentrantWrapperPtr() unsafe.Pointer
