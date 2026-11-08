// Package cppgo calls C++ from Go without cgo: no import "C", and nothing to
// build with cgo. It is the runtime layer of the project, and it never imports
// the generator (internal/cxx, internal/gen, cmd/cppgo), which takes libclang
// and is generation-time only. A program that uses a generated package depends
// on this package, internal/asm, internal/rtl, internal/callback and purego.
//
// A library is loaded with Open, a symbol is resolved with Library.Symbol, and
// the call is made through one of two carriers, both entered from package asm:
// a direct trampoline that calls the foreign address from a NOSPLIT assembly
// function on the goroutine's own stack, and a reentrant carrier that goes
// through runtime.cgocall because the callee may call back into Go.
//
// Both carriers cost the caller the same two things. The foreign frame runs on
// a stack the Go runtime cannot grow, so ProbeStack must run first, from the
// same goroutine and before any pointer into the caller's frame is taken. The
// goroutine also keeps its P for the duration of the call and is not a
// preemption point, so a long foreign call delays the stop-the-world phase of a
// collection.
//
// Handle and Object carry storage whose layout Go cannot see, Ptr, StrPtr and
// KeepAlive carry Go memory into a call and keep it there for as long as the
// call lasts, OpenFS loads a shared object embedded in the program, and
// LastError reads the message a failing function stored through the
// cppgo_last_error convention.
package cppgo

import (
	"sync"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// lastErrorSymbol is the conventional name of the thread-local error accessor
// every cppgo shim exports. It is looked up, not required: a library that
// never fails does not need one.
const lastErrorSymbol = "cppgo_last_error"

// Library is a loaded shared object, the handle every bound symbol is resolved
// through.
//
// It is safe for concurrent use. Symbol may be called from many goroutines
// while Close runs, and Close is serialized against them. Close does not wait
// for calls already running inside the library: closing a library while another
// goroutine is in a function resolved from it is a use-after-free, so a program
// closes a Library once, when the work that uses it is over.
type Library struct {
	mu     sync.RWMutex
	lib    *rtl.Library
	path   string
	closed bool
}

// Open loads the shared object at path and returns a handle to its symbols.
//
// The path goes to the dynamic loader unchanged. It may be absolute, a name the
// loader searches for such as "libm.so.6", or a relative path containing a
// slash. An empty path is refused, because the loader reads "" as the main
// program.
//
// Returns an *OpenError wrapping the loader's failure when the object cannot be
// loaded.
func Open(path string) (*Library, error) {
	if path == "" {
		return nil, &OpenError{Path: path, Err: errEmptyPath}
	}

	lib, err := rtl.Open(path)
	if err != nil {
		return nil, &OpenError{Path: path, Err: err}
	}

	return &Library{lib: lib, path: path}, nil
}

// Path returns the path the library was opened with.
func (l *Library) Path() string { return l.path }

// Close unloads the library. Every later Symbol call fails with ErrClosed, and
// so does a second Close.
func (l *Library) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return ErrClosed
	}
	// Mark closed before unloading: if dlclose itself fails the handle is
	// gone either way, and reporting the library as still open would invite
	// a use-after-free.
	l.closed = true

	return l.lib.Close()
}

// Symbol resolves name to the address of the foreign function or variable.
//
// The address points into the loaded library and has no Go pointer semantics,
// so it must not be stored anywhere the collector traces. uintptr is the only
// safe carrier.
//
// Returns a *SymbolError wrapping ErrClosed once Close has run, or the loader's
// error for a name the library does not export.
func (l *Library) Symbol(name string) (uintptr, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return 0, &SymbolError{Name: name, Err: ErrClosed}
	}

	addr, err := l.lib.Symbol(name)
	if err != nil {
		return 0, &SymbolError{Name: name, Err: err}
	}

	return addr, nil
}

// MustSymbol is Symbol for generated initialization code, which has no error
// path. It panics with the *SymbolError, because a symbol missing from the
// library the program was built against is a broken deployment rather than a
// runtime condition.
func (l *Library) MustSymbol(name string) uintptr {
	addr, err := l.Symbol(name)
	if err != nil {
		panic(err)
	}
	return addr
}

// LastError reads cppgo_last_error from lib and returns it as an error.
//
// It returns nil for a nil lib, for a library that exports no such symbol (a
// library that cannot fail) and for an empty C string (nothing has been
// recorded). The message is copied out before returning, because the C string
// belongs to the library and is only valid until the next call into it.
//
// The accessor is itself a foreign call, so LastError runs ProbeStack first.
func LastError(lib *Library) error {
	if lib == nil {
		return nil
	}

	addr, err := lib.Symbol(lastErrorSymbol)
	if err != nil {
		return nil
	}

	// The accessor is a foreign call like any other, so it needs the same
	// stack headroom.
	ProbeStack()
	raw, _ := asm.Call(rtl.Pointer(addr), &asm.Args{})

	msg := rtl.GoString(raw)
	if msg == "" {
		return nil
	}

	return &NativeError{Message: msg}
}
