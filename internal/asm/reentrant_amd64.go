//go:build amd64

package asm

import "unsafe"

// CallReentrant invokes the foreign function at fn with the arguments in a, on
// a thread that the Go runtime knows is inside a foreign call, so that the
// callee may call back into Go.
//
// This is the second call carrier, and the difference from Call is one runtime
// entry: the call is made through runtime.cgocall instead of the direct NOSPLIT
// trampoline. A Go function the callee invokes (a C function pointer, a
// std::function, a virtual override that calls a hook) then reaches
// runtime.cgocallback, which is the only path by which C code can enter Go
// with the runtime's knowledge: it needs the M to be in a cgo call
// (m.incgo), and that state is exactly what runtime.cgocall establishes. With
// the direct trampoline the same callback reaches cgocallback on an M that is
// not in a foreign call, and the runtime dies with "morestack on g0" at the
// exitsyscall that follows.
//
// The costs, measured rather than assumed:
//
//   - the C frames run on the M's system stack, not on the goroutine's stack,
//     so ProbeStack's 32 KiB reserve does not apply to them (see the deep
//     stack test for the budget that remains);
//   - the call costs what a cgo call costs, not what the direct trampoline
//     costs: 105 ns against 13.9 ns on the machine DESIGN.md's table was
//     measured on.
//
// The results are read from a.Ret and a.RetF, which the wrapper writes after
// the call: one carrier, one result path, even though cgocall itself has only
// an int32 result.
func CallReentrant(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	if a == nil {
		panic("cppgo: asm.CallReentrant: nil Args")
	}
	if fn == nil {
		panic("cppgo: asm.CallReentrant: nil fn")
	}

	b := &reentrantBlock{target: uintptr(fn), args: a}
	cgocall(reentrantWrapperPtr(), unsafe.Pointer(b))

	return a.Ret, a.RetF
}

// reentrantBlock is the single argument runtime.cgocall passes to the wrapper:
// the target and the register block to load for it.
type reentrantBlock struct {
	target uintptr
	args   *Args
}

// cgocall is runtime.cgocall: enters a foreign call (m.incgo, the g0 stack, the
// C ABI) and calls its first argument with its second as the argument. It is
// the same entry cgo-generated code uses, and reaching it by linkname is what
// keeps cgo out of the build: no import "C", no toolchain cgo at compile time.
//
//go:linkname cgocall runtime.cgocall
func cgocall(fn, arg unsafe.Pointer) int32
