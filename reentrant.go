package cppgo

import (
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// Args is one C argument frame: the six INTEGER and eight SSE argument
// registers of the System V AMD64 convention in I and F, and the two result
// registers, RAX and XMM0, in Ret and RetF.
//
// A generated binding fills I and F for the call and reads Ret or RetF after
// it; a callback reads its arguments from the same block and writes the one
// result register its C signature names. The layout is asm.Args, asserted by
// internal/asm's tests, and the type is named here because the generated
// package lives in another module and cannot import an internal package.
type Args = asm.Args

// CallReentrant invokes the foreign function at fn with the arguments in a,
// through runtime.cgocall, so that the callee may call back into Go.
//
// It is the carrier of every binding whose signature passes a Go function to
// C++: the runtime only accepts a callback on a thread that is already inside a
// foreign call, and this entry is what establishes that state. It costs what a
// cgo call costs (151 ns against 15.9 ns for the direct carrier on the machine
// DESIGN.md was measured on), so a binding with no callback in its signature
// does not use it.
//
// The results are read from a.Ret and a.RetF after the call returns.
func CallReentrant(fn uintptr, a *Args) {
	asm.CallReentrant(rtl.Pointer(fn), a)
}

// CallbackPanic is a panic a callback raised. The dispatcher recovers it before
// it can unwind into C++, zeroes the result and records it; the binding that
// passed the callback picks it up with TakeCallbackPanic and re-panics there.
type CallbackPanic = callback.Panic

// RegisterCallback makes fn callable from C++ and returns its code address.
//
// The address is a slot in the process-wide callback table and stays valid
// until ReleaseCallback is called for it. A binding registers the Go function,
// passes the address to the shim, and releases it after the foreign call
// returns, which is where the recorded panic is checked too.
func RegisterCallback(fn func(*Args)) (uintptr, error) {
	return callback.Register(fn)
}

// ReleaseCallback removes a callback registered by RegisterCallback, waiting for
// the calls already inside it to return.
func ReleaseCallback(code uintptr) error {
	return callback.Release(code)
}

// TakeCallbackPanic returns the panic the most recent callback captured and
// clears it. A binding calls it after the foreign call returns and re-panics
// with Value, so the panic reaches the caller instead of terminating the
// process.
func TakeCallbackPanic() *CallbackPanic {
	return callback.TakePanic()
}
