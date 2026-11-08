package cppgo

import (
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// Args is one C argument frame. It holds the six INTEGER and eight SSE argument
// registers of the System V AMD64 convention in I and F, and the two result
// registers, RAX and XMM0, in Ret and RetF.
//
// A generated binding fills I and F for the call and reads Ret or RetF after it.
// A callback reads its arguments from the same block and writes the one result
// register its C signature names. The layout is asm.Args, whose offsets
// internal/asm's tests assert. The alias is exported because a generated package
// lives in the user's module and cannot import an internal package.
type Args = asm.Args

// CallReentrant invokes the foreign function at fn with the arguments in a,
// through runtime.cgocall, so the callee may call back into Go.
//
// It is the carrier of every binding whose signature passes a Go function to
// C++: the runtime accepts a callback only on a thread that is already inside a
// foreign call, and this entry establishes that state. It costs what a cgo call
// costs, rather than what the direct trampoline costs: 151 ns against 15.9 ns
// per call in DESIGN.md's measurement of 2026-11-08. A binding with no callback
// in its signature takes the direct carrier instead.
//
// The results are read from a.Ret and a.RetF after the call returns.
func CallReentrant(fn uintptr, a *Args) {
	asm.CallReentrant(rtl.Pointer(fn), a)
}

// CallbackPanic is a panic a callback raised. The dispatcher recovers it before
// it can unwind into C++, zeroes the result and records it. The binding that
// passed the callback takes it with TakeCallbackPanic and re-panics there.
type CallbackPanic = callback.Panic

// RegisterCallback makes fn callable from C++ and returns its code address.
//
// The address is a slot in the process-wide callback table and stays valid until
// ReleaseCallback is called for it. A binding registers the Go function, passes
// the address to the shim, and releases it once the foreign call that could
// invoke it has returned.
func RegisterCallback(fn func(*Args)) (uintptr, error) {
	return callback.Register(fn)
}

// ReleaseCallback removes a callback registered by RegisterCallback, waiting for
// the calls already inside it to return so that no Go frame of fn is still
// running afterwards.
func ReleaseCallback(code uintptr) error {
	return callback.Release(code)
}

// TakeCallbackPanic returns the panic the most recent callback captured and
// clears it. A binding calls it after the foreign call returns and re-panics
// with Value, which delivers the panic to the caller instead of terminating the
// process.
func TakeCallbackPanic() *CallbackPanic {
	return callback.TakePanic()
}
