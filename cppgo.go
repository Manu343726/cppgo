// Package cppgo calls C++ from Go without cgo. There is no import "C", no cgo
// at build time, and no trip through the runtime's cgo call path.
//
// A library is loaded with Open, its symbols are resolved with
// Library.Symbol, and each call enters the System V C ABI from a small
// assembly trampoline (internal/asm). The shortcut carries two costs for the
// caller. A foreign call runs on the goroutine's own stack and must be
// preceded by ProbeStack. A call in flight delays a garbage collection cycle
// for as long as it lasts, because the goroutine keeps its P.
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
// A Library is safe for concurrent use. Symbols can be resolved from many
// goroutines at once, and Close is serialized against them. Close waits for no
// call that is already running inside the library. A program that
// closes a library while another goroutine is in a function it resolved from
// that library is asking for a use-after-free. A Library is normally closed
// once, after the work is done.
type Library struct {
	mu     sync.RWMutex
	lib    *rtl.Library
	path   string
	closed bool
}

// Open loads the shared object at path and returns a handle to its symbols.
//
// The path is passed to the dynamic loader as given. It may be absolute, a
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
// The returned address is a code or data pointer inside the loaded library,
// with no Go pointer semantics, and must not be stored in Go memory that the
// garbage collector has to trace.
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

// MustSymbol is Symbol for generated initialisation code. It panics on a
// missing symbol, because a symbol missing from the library the program was
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
// It returns nil when the library omits the symbol (a library that cannot
// fail) and when the C string is empty (no error has been recorded).
// The message is copied out immediately. The C string itself is only valid
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
