// Package cppgo calls C++ from Go without cgo: it loads a shared object, resolves
// the symbols a binding needs and makes the call, with no import "C" and nothing
// to build with cgo.
//
// A library is loaded with Open and a symbol resolved with Library.Symbol; the
// call itself is made by generated code, or by a hand-written binding through
// CallReentrant. Two rules belong to every caller. A foreign call runs on a stack
// Go cannot grow, so ProbeStack runs first and before any pointer into the
// caller's frame is taken. A foreign call is also not preempted, so a long one
// delays the runtime's own work until it returns.
//
// The package carries the values a binding moves across the boundary: Handle and
// Object for storage whose layout Go cannot see, Ptr, StrPtr and KeepAlive for Go
// memory lent to a call, OpenFS for a shared object embedded in the program, and
// LastError with the typed errors around it.
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
// so it must not be stored anywhere the collector traces. Hold it as a uintptr.
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
