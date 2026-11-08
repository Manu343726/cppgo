// Package callback implements the reverse direction of a cppgo call: a Go
// function handed to C++ as a function pointer and called by C++ while a
// Go→C++ call is in progress.
//
// The mechanism is the runtime's cgo callback path, reached without cgo at
// build time. A callback is an address in a table of five-byte CALL
// instructions (trampoline_amd64.s); the instruction a C caller lands on
// identifies the slot, the trampoline saves the fourteen argument registers and
// calls runtime.cgocallback through a copy of the runtime's host-ABI entry, and
// the runtime runs the Go function on the goroutine that owns the call, locked
// to the M, with the C frames left where they are.
//
// Two properties are the caller's to guarantee, and they are hard limits of the
// mechanism rather than choices:
//
//   - the C call that will invoke the callback must be made through
//     asm.CallReentrant, which enters C through runtime.cgocall. The runtime
//     only allows this entry on a thread that is inside a foreign call; on the
//     direct trampoline the same callback kills the process ("morestack on
//     g0", measured 2026-11-08). A binding that passes a function pointer
//     therefore has a carrier of its own.
//   - a callback takes at most six INTEGER and eight SSE arguments and returns
//     one value in RAX or XMM0. The trampoline reads the register file only:
//     stack-passed arguments and memory-class results are not carried.
//
// Registration is a slot in a fixed, process-wide table. The table is
// package-level state on purpose, and it is the only such state in cppgo: C
// holds a raw code address, and the dispatcher that address runs receives
// nothing but the register file, so nothing but a process-wide table can answer
// "which Go function is this". It is written under a mutex and read without one
// (a copy-on-write snapshot published atomically), and a slot is reclaimed only
// when no call is in flight on it.
package callback

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/Manu343726/cppgo/internal/asm"
)

// Slots is the number of callbacks the table can hold at once. It bounds the
// trampoline: one five-byte CALL instruction is emitted per slot.
const Slots = 64

var (
	// ErrFull is returned by Register when every slot is taken.
	ErrFull = fmt.Errorf("cppgo: callback table is full (%d slots)", Slots)
	// ErrUnknown is returned by Release for an address that is not a live
	// registration.
	ErrUnknown = errors.New("cppgo: not a live callback address")
	// ErrNilFunc is returned by Register for a nil function.
	ErrNilFunc = errors.New("cppgo: nil callback function")
)

// Args is one C argument frame: the six INTEGER and eight SSE argument
// registers of the System V AMD64 convention in I and F, and the two result
// registers, RAX and XMM0, in Ret and RetF. It is asm.Args, the same block a
// call loads its arguments from.
//
// A callback reads its arguments from I and F and writes its result to Ret (an
// integer, pointer or bool result) or RetF (a float result). Writing both is
// allowed and only the one the C signature names is read; leaving both zero is
// a zero result.
type Args = asm.Args

// Panic is a panic a callback raised, recovered at the trampoline boundary and
// handed back to the goroutine that owns the call.
type Panic struct {
	// Value is what the callback panicked with.
	Value any
	// Stack is the goroutine stack at the panic, taken with debug.Stack.
	Stack []byte
}

// Error implements error so a binding can return a callback panic as a value.
func (p *Panic) Error() string {
	return fmt.Sprintf("cppgo: panic in callback: %v", p.Value)
}

// Unwrap exposes the panic value to errors.Is and errors.As when it is an
// error.
func (p *Panic) Unwrap() error {
	err, _ := p.Value.(error)
	return err
}

// slot is one callback registration. active counts the calls currently inside
// the Go function; Release waits for it to reach zero before the slot is
// reused, so a callback is never swapped out from under a call that has already
// entered the trampoline.
type slot struct {
	fn     func(*Args)
	active atomic.Int64
}

// table is the process-wide registry: a copy-on-write snapshot of slots,
// published atomically so the callback path takes no lock (a callback that
// registers another callback must not deadlock against the path it runs on).
type table struct {
	mu      sync.Mutex // serializes writers only
	current atomic.Pointer[[Slots]*slot]
}

var registry = newTable()

func newTable() *table {
	t := &table{}
	t.current.Store(new([Slots]*slot))
	return t
}

// Register makes fn callable from C and returns its address.
//
// The address is a code address in this process, valid until Release is called
// for it; C may call it any number of times, from any thread, as long as the
// call that reaches it was made through asm.CallReentrant. Registering the same
// function twice gives two independent slots, because a caller may release one
// while another is still held by C++.
func Register(fn func(*Args)) (uintptr, error) {
	if fn == nil {
		return 0, ErrNilFunc
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	old := registry.current.Load()
	next := new([Slots]*slot)
	*next = *old

	index := -1
	for i, s := range next {
		if s == nil {
			index = i
			break
		}
	}
	if index < 0 {
		return 0, ErrFull
	}

	next[index] = &slot{fn: fn}
	registry.current.Store(next)

	return slotAddr(index), nil
}

// Release removes a callback registered by Register.
//
// It waits for the calls already inside fn to return, so a caller that is done
// with a callback can release it and know that no Go frame of it is still
// running. It says nothing about C: a C++ object that still holds the address
// and calls it after Release reaches the "released callback" panic described at
// dispatchFunc, which is a programmer error on the C++ side, not a
// use-after-free. Releasing an address twice, or one that was never registered,
// returns ErrUnknown.
func Release(code uintptr) error {
	index, ok := slotIndex(code)
	if !ok {
		return ErrUnknown
	}

	registry.mu.Lock()
	old := registry.current.Load()
	s := old[index]
	if s == nil {
		registry.mu.Unlock()
		return ErrUnknown
	}

	next := new([Slots]*slot)
	*next = *old
	next[index] = nil
	registry.current.Store(next)
	registry.mu.Unlock()

	// The slot is out of the snapshot, so no new call can find it; a call
	// already past the snapshot is counted in active.
	for s.active.Load() != 0 {
		runtime.Gosched()
	}

	return nil
}

// Lookup reports whether code is a live callback address. It exists for the
// tests and for a binding that wants to reject a stale pointer before handing
// it to C++.
func Lookup(code uintptr) bool {
	index, ok := slotIndex(code)
	if !ok {
		return false
	}
	return registry.current.Load()[index] != nil
}

// dispatch is the Go function the trampoline calls for every callback. It is
// kept in a variable so its code address can be loaded from assembly.
var dispatch = dispatchFunc

// dispatchFunc runs one callback.
//
// A slot that has been released while C still held its address has no Go
// function to run and no error channel through the C ABI, so it panics here:
// the panic is captured by the same recover as any other, reported through
// TakePanic, and the C caller receives a zero result. That is deliberate — a
// fabricated success would hide a use-after-release — and the message names the
// slot so the C++ side that kept the pointer can be found.
func dispatchFunc(f *callFrame) {
	s := registry.current.Load()[f.index]
	if s == nil {
		f.args.Ret, f.args.RetF = 0, 0
		pending.Store(&Panic{
			Value: fmt.Sprintf("cppgo: callback slot %d was released while C called it", f.index),
			Stack: debug.Stack(),
		})
		return
	}

	s.active.Add(1)
	defer s.active.Add(-1)

	// A panic must not unwind into C++: the unwinder has no frame of ours to
	// stop in and the process terminates. Capture it, zero the result, and
	// hand it to the goroutine that owns the call.
	defer func() {
		if r := recover(); r != nil {
			f.args.Ret, f.args.RetF = 0, 0
			pending.Store(&Panic{Value: r, Stack: debug.Stack()})
		}
	}()

	f.args.Ret, f.args.RetF = 0, 0
	s.fn(f.args)
}

// callFrame is the block the trampoline passes to dispatchFunc: the slot the C
// caller landed on and the argument frame it filled.
type callFrame struct {
	index uintptr
	args  *Args
}

// pending is the panic a callback captured. It is read and cleared by
// TakePanic on the goroutine that owns the call: the binding that passed a
// callback checks it when the foreign call returns, which is the first moment
// Go is running again on a stack where a panic may propagate.
var pending atomic.Pointer[Panic]

// TakePanic returns the panic the most recent callback captured and clears it.
// A binding that passes a callback to C++ calls TakePanic after the foreign call
// returns and re-panics with Value when it is non-nil, so a callback panic
// reaches the caller instead of terminating the process.
func TakePanic() *Panic {
	return pending.Swap(nil)
}
