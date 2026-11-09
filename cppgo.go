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
	"errors"
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
//
// Every name resolved through a Library is cached: the address, or the loader's
// error for a name the library does not export. The library is immutable while
// it is open, so a cached answer cannot go stale, and a program that reads its
// error state or closes objects in a loop stops paying a dlsym per call. The
// cache grows with the names asked for, which for generated code is the set of
// symbols its header declares. Close does not empty the cache — the addresses in
// it point into the unloaded object — because the closed flag is what a lookup
// consults first, and a closed library hands out neither an address nor a loader
// error from it.
type Library struct {
	mu     sync.RWMutex
	lib    *rtl.Library
	path   string
	closed bool
	// preloaded are the handles an embedded set was loaded with, oldest
	// first. They are closed with the library, after its own handle: a
	// dependency has to outlive the object that needs it.
	preloaded []*rtl.Library
	// symbols is the per-library cache, keyed by the name as it was asked
	// for. It is guarded by mu: read under the read lock, written under the
	// write lock by load. It is never nil once Open has returned.
	symbols map[string]symbolEntry
}

// symbolEntry is one cached resolution: the address the loader returned, or
// the error it reported for that name. An error is cached as it came back from
// the loader, so a name the library does not export costs one failed dlsym for
// the lifetime of the Library.
type symbolEntry struct {
	addr uintptr
	err  error
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

	return &Library{lib: lib, path: path, symbols: make(map[string]symbolEntry)}, nil
}

// Path returns the path the library was opened with.
func (l *Library) Path() string { return l.path }

// Close unloads the library. Every later Symbol call fails with ErrClosed, and
// so does a second Close.
//
// A library opened from an embedded set also releases the handles its set was
// preloaded with: they stay mapped exactly as long as this library is open.
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

	errs := make([]error, 0, 1+len(l.preloaded))
	errs = append(errs, l.lib.Close())
	for _, preloaded := range l.preloaded {
		errs = append(errs, preloaded.Close())
	}

	return errors.Join(errs...)
}

// Symbol resolves name to the address of the foreign function or variable.
//
// The address points into the loaded library and has no Go pointer semantics,
// so it must not be stored anywhere the collector traces. Hold it as a uintptr.
//
// The result is cached: a name is resolved once per Library, whether the loader
// found it or not, so repeated lookups cost a map read rather than a dlsym.
//
// Returns a *SymbolError wrapping ErrClosed once Close has run, or the loader's
// error for a name the library does not export.
func (l *Library) Symbol(name string) (uintptr, error) {
	addr, err := l.resolve(name)
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

// resolve is the lookup behind Symbol and LastError: the cached address or
// loader error for name, without the SymbolError wrapper. LastError reads the
// address and discards the reason, so it has no use for an error value.
//
// The closed flag is read in the same critical section as the cache entry, so a
// lookup that begins after Close returns sees the library closed and never the
// address Close invalidated.
func (l *Library) resolve(name string) (uintptr, error) {
	l.mu.RLock()
	entry, cached := l.symbols[name]
	closed := l.closed
	l.mu.RUnlock()

	if closed {
		return 0, ErrClosed
	}
	if cached {
		return entry.addr, entry.err
	}

	return l.load(name)
}

// load resolves a name through the loader, caches the outcome and returns it.
//
// It takes the write lock, which serializes it against Close: a load that wins
// the lock runs while the library is still open, and one that loses it runs
// after dlclose, where the loader itself answers ErrClosed instead of an
// address. Two goroutines that miss the same name at once both go to the loader
// and cache the same answer; that is a wasted dlsym once per name, never a
// wrong result.
func (l *Library) load(name string) (uintptr, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	addr, err := l.lib.Symbol(name)
	l.symbols[name] = symbolEntry{addr: addr, err: err}
	return addr, err
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

	addr, err := lib.resolve(lastErrorSymbol)
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
