package cppgo

import (
	"errors"
	"strconv"

	"github.com/Manu343726/cppgo/internal/rtl"
)

// ErrClosed is returned by every Library method that needs the loaded library
// once Close has run, and by a second Close. It is the same value the
// underlying loader shim uses, so errors.Is covers both layers.
var ErrClosed = rtl.ErrClosed

// OpenError reports a library that could not be loaded.
type OpenError struct {
	// Path is the path that was passed to Open.
	Path string
	// Err is the underlying loader error.
	Err error
}

// Error implements the error interface.
func (e *OpenError) Error() string {
	return "cppgo: open " + strconv.Quote(e.Path) + ": " + e.Err.Error()
}

// Unwrap exposes the underlying loader error.
func (e *OpenError) Unwrap() error { return e.Err }

// SymbolError reports a symbol that could not be resolved.
type SymbolError struct {
	// Name is the symbol that was requested.
	Name string
	// Err is the underlying loader error, or ErrClosed.
	Err error
}

// Error implements the error interface.
func (e *SymbolError) Error() string {
	return "cppgo: symbol " + strconv.Quote(e.Name) + ": " + e.Err.Error()
}

// Unwrap exposes the underlying loader error to errors.Is and errors.As.
func (e *SymbolError) Unwrap() error { return e.Err }

// ExtractError reports an embedded shared object that could not be
// materialised in the cache.
type ExtractError struct {
	// Name is the file name the object takes in the cache, or the name
	// OpenFS was asked for when no such entry exists.
	Name string
	// Dir is the cache directory the object was being written to, empty when
	// nothing was written.
	Dir string
	// Err is the underlying error: a file-system failure, a file that
	// changed between being hashed and being copied, or fs.ErrNotExist for a
	// name the file system does not contain.
	Err error
}

// Error implements the error interface.
func (e *ExtractError) Error() string {
	msg := "cppgo: extract " + strconv.Quote(e.Name)
	if e.Dir != "" {
		msg += " to " + strconv.Quote(e.Dir)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (e *ExtractError) Unwrap() error { return e.Err }

// ErrInsecureCache reports a cache path that is not a private directory: a
// regular file, a symbolic link, or a directory whose mode could not be
// tightened to 0700. OpenFS refuses to extract into it rather than write a
// shared object somewhere another user could replace.
var ErrInsecureCache = errors.New("cache path is not a private directory")

// NativeError is a failure reported by the foreign library through the
// cppgo_last_error convention. Its message is the C string verbatim; the
// generated wrappers add the symbol name on top.
type NativeError struct {
	// Message is the text the foreign library stored, without the trailing
	// NUL.
	Message string
}

// Error implements the error interface.
func (e *NativeError) Error() string { return e.Message }

// errEmptyPath is what Open returns for "", which would otherwise make dlopen
// hand back the main program.
var errEmptyPath = errors.New("empty library path")

// errNilFileSystem is what OpenFS returns for a nil fs.FS, which would
// otherwise panic somewhere inside the walk.
var errNilFileSystem = errors.New("nil file system")
