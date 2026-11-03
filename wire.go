package cppgo

import (
	"runtime"
	"unsafe"
)

// Ptr returns a pointer to the first element of s, the form a []T takes on its
// way across the boundary, or nil when s is empty.
//
// The pointer borrows the slice's backing array: it stays valid only for the
// duration of the foreign call, the callee must not retain it, and the caller
// must keep s alive across the call with KeepAlive.
func Ptr[T any](s []T) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(&s[0])
}

// StrPtr returns a pointer to the bytes of s, for a function that takes a
// string as a (pointer, length) pair.
//
// The pointer borrows the string: it stays valid only for the duration of the
// foreign call, the callee must not retain it, and the caller must keep s
// alive with KeepAlive. Strings are not NUL-terminated; a function that wants
// a C string needs an explicit terminator in a []byte.
func StrPtr(s string) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(unsafe.StringData(s))
}

// KeepAlive keeps its arguments reachable until the call returns, so that the
// memory a foreign call was handed is not collected or moved while the callee
// is still reading it.
//
// Call it after the trampoline, never before:
//
//	cppgo.ProbeStack()
//	n, _ := asm.Call(...)
//	cppgo.KeepAlive(buf)
func KeepAlive(v ...any) {
	runtime.KeepAlive(v)
}
