//go:build amd64

package asm

import "unsafe"

// CallReentrant invokes the foreign function at fn with the arguments in a, on a
// thread the Go runtime knows is inside a foreign call, so the callee may call
// back into Go.
//
// This is the second carrier, and it differs from Call in one runtime entry: the
// call goes through runtime.cgocall. A Go function the callee invokes (a C
// function pointer, a std::function, a virtual override that calls a hook) then
// reaches runtime.cgocallback, which is the only path by which C code enters Go
// with the runtime's knowledge. That entry needs the M to be in a cgo call
// (m.incgo), and runtime.cgocall establishes exactly that state. On the direct
// trampoline the same callback reaches cgocallback on an M that is not in a
// foreign call, and the runtime dies with "morestack on g0" at the exitsyscall
// that follows.
//
// Its costs are measured rather than assumed. The C frames run on the M's system
// stack instead of the goroutine's, so the 32 KiB ProbeStack guarantees do not
// apply to them (the deep stack test measures the budget that remains), and the
// call costs what a cgo call costs: 151 ns against 15.9 ns for the direct
// carrier on the machine DESIGN.md was measured on.
//
// The results are read from a.Ret and a.RetF, which the wrapper writes after the
// call. That keeps one result path for both carriers, even though cgocall itself
// has only an int32 result.
//
// It is nosplit, and so is the wrapper and runtime.cgocall: the caller fills the
// argument block and then calls here, and a stack growth in between would move
// every Go object the block holds a uintptr of, leaving C reading the old
// address. Nothing may grow this goroutine's stack between the frame being
// filled and the callee reading it. The nil checks below panic, which is an
// abort rather than a call that has to return, so the growth the panic needs is
// nobody's problem.
//
//go:nosplit
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

// cgocall is runtime.cgocall: it enters a foreign call (m.incgo, the g0 stack,
// the C ABI) and calls its first argument with its second as the argument. It is
// the same entry cgo-generated code uses, and reaching it by linkname is what
// keeps cgo out of the build: no import "C", and no toolchain cgo at compile
// time.
//
//go:linkname cgocall runtime.cgocall
func cgocall(fn, arg unsafe.Pointer) int32
