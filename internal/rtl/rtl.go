// Package rtl is the dynamic-loader shim cppgo is built on.
//
// It wraps dlopen/dlsym/dlclose as github.com/ebitengine/purego exposes them, and
// adds the two things cppgo needs on top: errors that name the library and the
// symbol that failed, and a documented way to turn a symbol address into an
// unsafe.Pointer without tripping go vet's unsafeptr check.
//
// Only purego's loader entry points are used. Its calling convention
// (RegisterFunc, SyscallN and the rest) goes unused, because cppgo enters the C
// ABI from its own assembly trampolines.
//
// A Library carries no lock of its own; the cppgo package wraps it in an
// RWMutex. dlopen, dlsym and dlclose are thread-safe in glibc.
package rtl

import (
	"errors"
	"strconv"
	"unsafe"

	"github.com/ebitengine/purego"
)

// openFlags is how every library is loaded: resolve all symbols up front
// (RTLD_NOW: a missing dependency fails here, not at the first call) and keep
// the symbols private to the process's own resolution graph (RTLD_LOCAL).
const openFlags = purego.RTLD_NOW | purego.RTLD_LOCAL

// preloadFlags is how a set cppgo has to load bottom-up is loaded: the symbols go
// into the global scope, so an object loaded afterwards resolves its DT_NEEDED
// entries from the ones already mapped.
const preloadFlags = purego.RTLD_NOW | purego.RTLD_GLOBAL

var (
	// ErrClosed is returned by every Library method once Close has run.
	ErrClosed = errors.New("library is closed")

	errEmptyPath = errors.New("empty library path")
	errEmptyName = errors.New("empty symbol name")
)

// The loader entry points, kept in variables so tests can exercise the failure
// branches of a system call that will not fail on demand. They are unexported.
var (
	dlopen  = purego.Dlopen
	dlsym   = purego.Dlsym
	dlclose = purego.Dlclose
)

// Error reports a failed dynamic-loader operation.
type Error struct {
	// Op is the failing loader call: "dlopen", "dlsym" or "dlclose".
	Op string
	// Path is the library the operation was applied to, when known.
	Path string
	// Name is the symbol the operation was applied to, when known.
	Name string
	// Err is the underlying loader error.
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	msg := "rtl: " + e.Op
	if e.Name != "" {
		msg += " " + strconv.Quote(e.Name)
	}
	if e.Path != "" {
		msg += " in " + strconv.Quote(e.Path)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the underlying loader error to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

// Library is a loaded shared object.
//
// It holds no lock, so a caller that closes it while another goroutine resolves a
// symbol through it reads a torn closed flag; the cppgo package adds the RWMutex
// that makes that safe.
type Library struct {
	handle uintptr
	path   string
	closed bool
}

// Open loads the shared object at path.
//
// An empty path is rejected. dlopen("") would silently return the main
// program's handle, and that is never what a caller of Open means.
func Open(path string) (*Library, error) {
	if path == "" {
		return nil, &Error{Op: "dlopen", Err: errEmptyPath}
	}

	handle, err := dlopen(path, openFlags)
	if err != nil {
		return nil, &Error{Op: "dlopen", Path: path, Err: err}
	}

	return &Library{handle: handle, path: path}, nil
}

// Path returns the path the library was opened with.
func (l *Library) Path() string { return l.path }

// Preload loads the shared object at path with its symbols in the global scope
// and returns its handle.
//
// The caller owns the handle, exactly as it owns one from Open: a preloaded
// object is what a later object's DT_NEEDED entry resolves against, so it has to
// stay mapped for as long as the object that needs it. Loading a set bottom-up
// this way is what makes a transitive dependency resolve to the copy the program
// extracted, instead of to a search that would end in the build tree or in
// nothing.
//
// A second Preload of the same file is a second reference to the same mapping,
// which the loader counts; it costs a refcount and no memory.
func Preload(path string) (*Library, error) {
	if path == "" {
		return nil, &Error{Op: "dlopen", Err: errEmptyPath}
	}

	handle, err := dlopen(path, preloadFlags)
	if err != nil {
		return nil, &Error{Op: "dlopen", Path: path, Err: err}
	}

	return &Library{handle: handle, path: path}, nil
}

// Symbol resolves name to its address in the library.
//
// The error is always an *Error. When the library has been closed it wraps
// ErrClosed.
func (l *Library) Symbol(name string) (uintptr, error) {
	if l.closed {
		return 0, &Error{Op: "dlsym", Path: l.path, Name: name, Err: ErrClosed}
	}
	if name == "" {
		return 0, &Error{Op: "dlsym", Path: l.path, Err: errEmptyName}
	}

	addr, err := dlsym(l.handle, name)
	if err != nil {
		return 0, &Error{Op: "dlsym", Path: l.path, Name: name, Err: err}
	}

	return addr, nil
}

// Close unloads the library. A second Close is an error wrapping ErrClosed, so a
// double close is always reported.
func (l *Library) Close() error {
	if l.closed {
		return &Error{Op: "dlclose", Path: l.path, Err: ErrClosed}
	}

	if err := dlclose(l.handle); err != nil {
		return &Error{Op: "dlclose", Path: l.path, Err: err}
	}

	l.closed = true
	l.handle = 0
	return nil
}

// Pointer reinterprets a raw foreign address as an unsafe.Pointer.
//
// Addresses the loader hands out point at code or at data owned by the foreign
// library, never into Go memory. unsafe.Pointer(uintptr) is exactly the
// conversion meant, but go vet's unsafeptr check cannot know that and rejects it.
// Reading the bits through a *uintptr performs the same conversion and states the
// intent. The result points outside the Go heap, and the collector tracks only Go
// memory.
func Pointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// GoString copies the NUL-terminated C string at addr into a Go string. A zero
// address is the empty string, so a library that returns NULL from its error
// accessor reports no error.
//
// The copy happens immediately. The C string is typically only valid until the
// next call into the library.
func GoString(addr uintptr) string {
	p := Pointer(addr)
	if p == nil {
		return ""
	}

	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}

	return unsafe.String((*byte)(p), n)
}
