package cppgo

import (
	"runtime"
	"unsafe"
)

// Ptr returns a pointer to the first element of s, the form a slice takes on its
// way across the boundary, or nil when s is empty.
//
// The pointer borrows the slice's backing array. It stays valid for the duration
// of the foreign call only, the callee must not retain it, and the caller keeps
// s alive across the call with KeepAlive.
func Ptr[T any](s []T) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(&s[0])
}

// StrPtr returns a pointer to the bytes of s, for a callee that takes a string
// as a (pointer, length) pair.
//
// The pointer borrows the string and carries the same rules as Ptr. Go strings
// are not NUL-terminated, so a callee that wants a C string needs an explicit
// terminator in a []byte.
func StrPtr(s string) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(unsafe.StringData(s))
}

// KeepAlive keeps its arguments reachable until the call returns, so the memory
// a foreign call was handed stays alive and in place while the callee is still
// reading it.
//
// Call it after the call, never before. Probing, forming the pointers and making
// the call come first, in that order:
//
//	cppgo.ProbeStack()
//	p := cppgo.StrPtr(name)
//	callSetName(p, uintptr(len(name)))
//	cppgo.KeepAlive(name)
func KeepAlive(v ...any) {
	runtime.KeepAlive(v)
}
