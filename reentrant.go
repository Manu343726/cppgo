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
// register its C signature names. The layout is fixed; a change to it breaks both
// directions at once.
type Args = asm.Args

// CallReentrant invokes the foreign function at fn with the arguments in a, in the
// state a callback needs: the callee may call back into Go.
//
// It is the call to use whenever a Go function is passed to C++, as a function
// pointer or a std::function, because no other call may invoke a callback. A call
// that passes none does not need it, and costs less.
//
// The results are read from a.Ret and a.RetF after the call returns.
//
// It does not grow the goroutine's stack, on purpose: a binding fills the
// argument block and calls here, and a stack growth between the two would move
// every Go object that block names, leaving the callee reading the old address.
// The reentrant carrier therefore hands C a pinned copy of any Go memory the
// call may still be reading when a callback runs.
//
//go:nosplit
func CallReentrant(fn uintptr, a *Args) {
	asm.CallReentrant(rtl.Pointer(fn), a)
}

// CallbackPanic is a panic a callback raised.
//
// It does not unwind into C++, which would terminate the process. It is recorded,
// and the binding that passed the callback takes it with TakeCallbackPanic once
// the call has returned and re-panics there, so it reaches the code that made the
// call.
type CallbackPanic = callback.Panic

// RegisterCallback makes fn callable from C++ and returns its code address.
//
// The address stays valid until ReleaseCallback is called for it. A binding
// registers the Go function, passes the address to C++, and releases it once the
// call that could invoke it has returned.
func RegisterCallback(fn func(*Args)) (uintptr, error) {
	return callback.Register(fn)
}

// ReleaseCallback removes a callback registered by RegisterCallback, waiting for
// the calls already inside it to return so that no Go frame of fn is still
// running afterwards.
func ReleaseCallback(code uintptr) error {
	return callback.Release(code)
}

// TakeCallbackPanic returns the panic the most recent callback captured and clears
// it. A binding calls it after the foreign call returns and re-panics with Value,
// which delivers the panic to its caller instead of terminating the process.
func TakeCallbackPanic() *CallbackPanic {
	return callback.TakePanic()
}
