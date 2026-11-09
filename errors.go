package cppgo

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Manu343726/cppgo/internal/rtl"
)

// ErrClosed is what every Library method returns once Close has run, and what a
// second Close returns. errors.Is matches it wherever it is wrapped.
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
// materialized in the cache.
type ExtractError struct {
	// Name is the file name the object takes in the cache, or the name
	// OpenFS was asked for when no such entry exists.
	Name string
	// Dir is the cache directory the object was being written to, empty when
	// nothing was written.
	Dir string
	// Err is the underlying error: a file-system failure, a file that
	// changed while it was being written out, or fs.ErrNotExist for a name
	// the file system does not contain.
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
// regular file, a symbolic link, or a directory that could not be made private
// (0700). OpenFS refuses to extract into it, because another local user could
// then replace a shared object this process maps executable.
var ErrInsecureCache = errors.New("cache path is not a private directory")

// SearchError reports a library name that no explicit override and no dynamic
// loader search resolved. It names every path the overrides covered and the two
// variables that would have changed the answer, because the fix is a deployment
// step: put the object where the search looks, or point a variable at it.
type SearchError struct {
	// Name is the bare name that was searched for.
	Name string
	// Paths are the exact paths tried before the loader's own search, in
	// order: one per CPPGO_LIBRARY_PATH entry, or the generated package's
	// CppgoLibraryPath when it had one.
	Paths []string
	// Err is the dynamic loader's error for Name, its own search having been
	// the last attempt.
	Err error
}

// Error implements the error interface.
func (e *SearchError) Error() string {
	msg := "cppgo: open " + strconv.Quote(e.Name) + ": not found"
	if len(e.Paths) > 0 {
		quoted := make([]string, len(e.Paths))
		for i, path := range e.Paths {
			quoted[i] = strconv.Quote(path)
		}
		msg += " in " + strings.Join(quoted, ", ")
	}
	msg += "; the dynamic loader searched LD_LIBRARY_PATH, DT_RUNPATH, /etc/ld.so.cache and the default directories and reported: " + e.Err.Error()
	msg += "; set " + LibraryPathEnv + " to a colon-separated list of directories, or CppgoLibraryPath to one library"

	return msg
}

// Unwrap exposes the loader's error to errors.Is and errors.As.
func (e *SearchError) Unwrap() error { return e.Err }

// DependencyError reports a DT_NEEDED entry of an embedded object that neither
// the embedded set nor the dynamic loader's search provides.
//
// It is raised while the set is preloaded, before the object that needs the
// dependency is mapped, so the program fails with a statement of what is absent
// rather than with a loader message about a file nobody shipped. Both ways out
// are named because which one to take is a packaging decision.
type DependencyError struct {
	// Object is the embedded file that names the dependency.
	Object string
	// Name is the DT_NEEDED entry that resolved to nothing.
	Name string
}

// Error implements the error interface.
func (e *DependencyError) Error() string {
	return "cppgo: dependency " + strconv.Quote(e.Name) + " of " + strconv.Quote(e.Object) +
		" is missing: the embedded set does not carry it and the dynamic loader's search " +
		"(LD_LIBRARY_PATH, DT_RUNPATH, /etc/ld.so.cache and the default directories) does not find it; " +
		"package it with the library or install it where the loader looks"
}

// NativeError is a failure the foreign library reported through the
// cppgo_last_error convention. Its message is the C string verbatim. A generated
// wrapper adds the symbol name on top, as a CallError.
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
