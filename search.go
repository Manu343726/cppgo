package cppgo

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LibrarySearch is how a cppgo binding resolves the user's shared object: the
// explicit overrides first, the embedded set next, the dynamic loader's own
// search last.
//
// The order is the one a Linux deployment expects, with the cppgo-specific
// variables as the higher-priority overrides:
//
//  1. Override, an exact path — the generated package's CppgoLibraryPath,
//     which is what a development build sets before Open.
//  2. Each entry of CPPGO_LIBRARY_PATH, in order. An entry is a directory
//     searched for Name, or the object itself, which is the spelling
//     ADR-0003 gives the same variable.
//  3. FS, when the package embeds the C++ side: the set is extracted to the
//     private cache and loaded from there, as OpenFS does.
//  4. Name itself, passed bare to the loader, so LD_LIBRARY_PATH, DT_RUNPATH,
//     DT_RPATH, ld.so.cache and the default directories all apply.
//
// There is no fifth step on purpose: the directory an object was built in, or
// the directory the shim happens to sit in, is never a place to look. A program
// does not run where it was built, and the embedded set is how a deployment
// carries its C++ side.
//
// Name is a name, never a path: making it absolute first would bypass every one
// of the conventions above.
type LibrarySearch struct {
	// Name is the bare library name: the soname the shim records, or the file
	// name of an object that declares none.
	Name string
	// Override is an exact path tried before anything else. Empty means no
	// override.
	Override string
	// FS is the embedded file system holding the user's library and the shim
	// cppgo built, or nil when the package does not embed them.
	FS fs.FS
}

// Open resolves the library and loads it, the first match winning.
//
// A file that was found but could not be loaded is reported as the loader
// reported it, an *OpenError naming the path. A name that nothing held is a
// *SearchError that lists every path the overrides covered and names the two
// variables that would have overridden the search.
func (s LibrarySearch) Open() (*Library, error) {
	if s.Name == "" {
		return nil, &OpenError{Path: s.Name, Err: errEmptyPath}
	}

	path, tried := explicitPath(s.Override, s.Name)
	if path != "" {
		return Open(path)
	}

	if s.FS != nil {
		return openEmbedded(s.FS, s.Name)
	}

	lib, err := Open(s.Name)
	if err != nil {
		return nil, &SearchError{Name: s.Name, Paths: tried, Err: err}
	}

	return lib, nil
}

// explicitPath resolves name against the explicit overrides: override, when the
// generated package set it, and then every entry of CPPGO_LIBRARY_PATH in order.
//
// It returns the path that holds the name, and the paths it tried so a search
// that found nothing can say where it looked. A name that is found is loaded by
// the caller and a load failure is reported there; a path is never skipped
// because the object at it turns out to be broken.
func explicitPath(override, name string) (path string, tried []string) {
	if override != "" {
		return override, []string{override}
	}

	for _, entry := range libraryPathEntries(os.Getenv(LibraryPathEnv)) {
		candidate := entryCandidate(entry, name)
		tried = append(tried, candidate)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, tried
		}
	}

	return "", tried
}

// libraryPathEntries splits a CPPGO_LIBRARY_PATH value into its entries.
//
// An empty value is no entries, and an empty entry between separators is
// dropped. glibc reads an empty LD_LIBRARY_PATH entry as the current directory;
// nothing is searched implicitly here, because the directory a program happens
// to be started in is not a place a library should come from.
func libraryPathEntries(value string) []string {
	if value == "" {
		return nil
	}

	var entries []string
	for _, entry := range strings.Split(value, string(os.PathListSeparator)) {
		if entry != "" {
			entries = append(entries, entry)
		}
	}

	return entries
}

// entryCandidate is the path one CPPGO_LIBRARY_PATH entry stands for: the entry
// itself when it names an object rather than a directory, and entry/name when it
// names a directory.
//
// The object spelling is the one ADR-0003 gives the variable, so a deployment
// that already names the file keeps working; a list of directories is what
// LD_LIBRARY_PATH has taught every operator to write. An entry that does not
// exist is a directory unless its own name is the name being searched for, which
// is the reading that makes a stale exact-path override say so in its error.
func entryCandidate(entry, name string) string {
	info, err := os.Stat(entry)
	if err == nil {
		if info.IsDir() {
			return filepath.Join(entry, name)
		}

		return entry
	}
	if filepath.Base(entry) == name {
		return entry
	}

	return filepath.Join(entry, name)
}
