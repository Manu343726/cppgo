// Package cppgo calls C++ from Go without cgo: no import "C", no cgo at build
// time, and no trip through the runtime's cgo call path.
//
// A library is loaded with Open, its symbols are resolved with
// Library.Symbol, and each call enters the System V C ABI from a small
// assembly trampoline (internal/asm) instead of through runtime.cgocall. The
// two costs of that shortcut are paid by the caller: a foreign call runs on the
// goroutine's own stack, so it must be preceded by ProbeStack, and a call in
// flight delays a garbage collection cycle for as long as it lasts (the P is
// not released).
//
// Wire helpers (Ptr, StrPtr, KeepAlive) exist so hand-written bindings and
// generated code can pass Go memory across the boundary safely, and LastError
// reads the message a failing function stored through the cppgo_last_error
// convention.
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
// A Library is safe for concurrent use: symbols can be resolved from many
// goroutines at once, and Close is serialised against them. What Close cannot
// do is wait for calls that are already running inside the library: a program
// that closes a library while another goroutine is in a function it resolved
// from it is asking for a use-after-free, so a Library is normally closed once,
// after the work is done.
type Library struct {
	mu     sync.RWMutex
	lib    *rtl.Library
	path   string
	closed bool
}

// Open loads the shared object at path and returns a handle to its symbols.
//
// The path is passed to the dynamic loader as given; it may be absolute, a
// name on the loader's search path such as "libm.so.6", or a relative path
// containing a slash.
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
// The returned address is a code or data pointer inside the loaded library. It
// is not a Go pointer and must not be stored in Go memory that the garbage
// collector has to trace.
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

// MustSymbol is Symbol for generated initialisation code: it panics instead of
// returning an error, because a missing symbol in a library the program was
// built against is a fatal configuration mistake.
func (l *Library) MustSymbol(name string) uintptr {
	addr, err := l.Symbol(name)
	if err != nil {
		panic(err)
	}
	return addr
}

// LastError reads cppgo_last_error from lib and returns it as an error.
//
// It returns nil when the library does not export the symbol (a library that
// cannot fail) and when the C string is empty (no error has been recorded).
// The message is copied out immediately; the C string itself is only valid
// until the next call into the library.
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
