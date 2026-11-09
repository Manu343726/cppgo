// Package elfdeps resolves the DT_NEEDED closure of a shared object the way the
// Linux dynamic loader will at run time.
//
// It is the generation-time half of embedding the C++ side: cppgo needs the
// complete dependency tree of the user's library to decide what a program
// packages, and it needs the load order to preload a set it does embed. The walk
// is pure Go — debug/elf for the object headers, the documented ld.so search for
// the names — so it needs no cgo and no ldd process.
package elfdeps

import (
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemCache is the file glibc's loader keeps its directory cache in.
const SystemCache = "/etc/ld.so.cache"

// Object is the part of an ELF shared object the loader's search depends on.
type Object struct {
	// Path is the file it was read from.
	Path string
	// SOName is DT_SONAME, empty when the object declares none. A dependency
	// is matched against a loaded object by this name, which is why an object
	// without one cannot satisfy a DT_NEEDED entry.
	SOName string
	// Needed is DT_NEEDED, in the order the loader processes it.
	Needed []string
	// RPath is DT_RPATH split into directories. It is searched only when the
	// object has no DT_RUNPATH.
	RPath []string
	// RunPath is DT_RUNPATH split into directories.
	RunPath []string
	// Class is the ELF class: a cached object of the other word size is not
	// this object's dependency.
	Class elf.Class
}

// Read reads the dynamic section of the shared object at path.
func Read(path string) (*Object, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("elfdeps: read %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if f.Type != elf.ET_DYN {
		return nil, fmt.Errorf("elfdeps: %q is not a shared object: ELF type %s, want %s", path, f.Type, elf.ET_DYN)
	}

	obj := &Object{Path: path, Class: f.Class}
	obj.SOName = firstDynamic(f, elf.DT_SONAME)
	obj.RPath = splitPath(firstDynamic(f, elf.DT_RPATH))
	obj.RunPath = splitPath(firstDynamic(f, elf.DT_RUNPATH))
	// A file with no dynamic section is not a shared object the loader can
	// use, and DynString says so with an error; the empty list is what a
	// caller can act on.
	obj.Needed, _ = f.DynString(elf.DT_NEEDED)

	return obj, nil
}

// firstDynamic returns the first value of tag, or "" when the object has none.
func firstDynamic(f *elf.File, tag elf.DynTag) string {
	values, err := f.DynString(tag)
	if err != nil || len(values) == 0 {
		return ""
	}

	return values[0]
}

// splitPath splits a DT_RPATH or DT_RUNPATH value into directories. The
// separator is ':' in the ELF format itself, whatever the host's list separator
// is.
func splitPath(value string) []string {
	if value == "" {
		return nil
	}

	var dirs []string
	for _, dir := range strings.Split(value, ":") {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// Resolver is the dynamic loader's search, as the walk applies it. Its zero
// value is the running process's own search: LD_LIBRARY_PATH, the system cache
// and the platform's default directories.
type Resolver struct {
	// LibraryPath is LD_LIBRARY_PATH split into directories. nil reads the
	// process's own value; an empty non-nil slice searches none, which is what
	// a test that must not depend on the environment passes.
	LibraryPath []string
	// Cache is the ld.so cache file to consult. Empty means SystemCache, and a
	// path that does not exist is a cache with nothing in it.
	Cache string
	// DefaultDirs are the directories searched last. nil means the platform's
	// own list.
	DefaultDirs []string
}

// SystemDefaultDirs returns the directories the loader searches after the cache.
//
// The multiarch directories are not listed: the cache is where a system with
// ldconfig keeps them, exactly as the loader finds them.
func SystemDefaultDirs() []string {
	return []string{"/lib64", "/usr/lib64", "/lib", "/usr/lib"}
}

// libraryPath is the LD_LIBRARY_PATH this resolver searches.
func (r Resolver) libraryPath() []string {
	if r.LibraryPath != nil {
		return r.LibraryPath
	}

	return splitPath(os.Getenv("LD_LIBRARY_PATH"))
}

// cachePath is the cache file this resolver consults.
func (r Resolver) cachePath() string {
	if r.Cache != "" {
		return r.Cache
	}

	return SystemCache
}

// defaults are the directories this resolver searches last.
func (r Resolver) defaults() []string {
	if r.DefaultDirs != nil {
		return r.DefaultDirs
	}

	return SystemDefaultDirs()
}

// dirs is the search order for a dependency of requester: its DT_RPATH when it
// has no DT_RUNPATH, LD_LIBRARY_PATH, its DT_RUNPATH, and then the cache and the
// default directories, which Resolve consults itself.
func (r Resolver) dirs(requester *Object) []string {
	var dirs []string
	if requester != nil {
		if len(requester.RunPath) == 0 {
			dirs = append(dirs, expandOrigin(requester, requester.RPath)...)
		}
		dirs = append(dirs, r.libraryPath()...)
		dirs = append(dirs, expandOrigin(requester, requester.RunPath)...)
	} else {
		dirs = append(dirs, r.libraryPath()...)
	}

	return dirs
}

// expandOrigin replaces $ORIGIN and ${ORIGIN} with the directory the object was
// read from, which is where the loader resolves them from. $LIB and $PLATFORM
// are left alone: they name a platform layout cppgo does not second-guess.
func expandOrigin(obj *Object, dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}

	origin := filepath.Dir(obj.Path)
	expanded := make([]string, len(dirs))
	for i, dir := range dirs {
		dir = strings.ReplaceAll(dir, "${ORIGIN}", origin)
		expanded[i] = strings.ReplaceAll(dir, "$ORIGIN", origin)
	}

	return expanded
}

// Resolve resolves one DT_NEEDED name the way the loader will for requester.
func (r Resolver) Resolve(requester *Object, name string) (string, bool) {
	// A name with a slash is a path: the loader uses it as given and does not
	// search for it.
	if strings.ContainsRune(name, '/') {
		return name, fileExists(name)
	}

	for _, dir := range r.dirs(requester) {
		candidate := filepath.Join(dir, name)
		if fileExists(candidate) {
			return candidate, true
		}
	}

	if path, ok := r.lookupCache(name, requester.Class); ok {
		return path, true
	}

	for _, dir := range r.defaults() {
		candidate := filepath.Join(dir, name)
		if fileExists(candidate) {
			return candidate, true
		}
	}

	return "", false
}

// fileExists reports whether path names a regular file, which is the only shape
// the loader will map.
func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

// Dependency is one object in a resolved closure, or one DT_NEEDED name that
// resolved to nothing and whose Path is therefore empty.
type Dependency struct {
	// Name is the DT_NEEDED entry: a soname, or a file name for an object that
	// declares none.
	Name string
	// Path is the file the name resolved to.
	Path string
	// SOName is the resolved object's own DT_SONAME.
	SOName string
	// NeededBy is the file of the object that named this dependency; it is the
	// root's path for a direct dependency.
	NeededBy string
	// Runtime reports whether the dependency is part of the Linux/C/C++
	// runtime rather than something the program has to ship.
	Runtime bool
}

// Tree is the resolved DT_NEEDED closure of one shared object.
type Tree struct {
	// Path is the object the walk started from.
	Path string
	// Order is the closure in load order: a dependency is listed before the
	// object that needs it, which is the order a preload has to use.
	Order []Dependency
	// Missing is every name that resolved to nothing, in the order the walk
	// reached it.
	Missing []Dependency
}

// Runtime returns the closure's Linux/C/C++ runtime objects, in load order.
func (t *Tree) Runtime() []Dependency { return t.kind(true) }

// NonRuntime returns everything that is not part of that runtime, in load
// order: the objects a deployment has to carry itself.
func (t *Tree) NonRuntime() []Dependency { return t.kind(false) }

// kind filters the ordered closure by classification.
func (t *Tree) kind(runtime bool) []Dependency {
	var out []Dependency
	for _, dep := range t.Order {
		if dep.Runtime == runtime {
			out = append(out, dep)
		}
	}

	return out
}

// Tree walks the DT_NEEDED closure of the shared object at path.
//
// The walk is depth first and appends each dependency after its own, so Order is
// a load order. A name that resolves to nothing is recorded in Missing and does
// not abort the walk: a caller that must refuse the set says why with the whole
// list in hand. An error is a file that could not be read or is not a shared
// object, never a name that did not resolve.
func (r Resolver) Tree(path string) (*Tree, error) {
	root, err := Read(path)
	if err != nil {
		return nil, err
	}

	tree := &Tree{Path: path}

	// state records what the walk has done with a file: 1 while it is on the
	// stack, 2 once it has been placed. The loader copes with a cycle, and so
	// does this.
	state := map[string]int{}

	var walk func(obj *Object)
	walk = func(obj *Object) {
		state[obj.Path] = 1
		for _, name := range obj.Needed {
			depPath, resolved := r.Resolve(obj, name)
			dep := Dependency{
				Name:     name,
				Path:     depPath,
				NeededBy: obj.Path,
				Runtime:  IsRuntime(name),
			}
			if !resolved {
				tree.Missing = append(tree.Missing, dep)
				continue
			}
			if state[depPath] != 0 {
				continue
			}

			child, err := Read(depPath)
			if err != nil {
				// Something found the name but it is not a shared object;
				// the walk records it exactly as it records a missing one.
				dep.Path = ""
				tree.Missing = append(tree.Missing, dep)
				continue
			}

			dep.SOName = child.SOName
			walk(child)
			state[depPath] = 2
			tree.Order = append(tree.Order, dep)
		}
	}
	walk(root)

	return tree, nil
}
