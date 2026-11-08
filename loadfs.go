package cppgo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LibraryPathEnv is the environment variable that overrides OpenFS with an
// explicit library path. When it is non-empty OpenFS extracts nothing and
// loads that path instead, which is how a development build points the same
// program at a library the C++ build left in place:
//
//	CPPGO_LIBRARY_PATH=/path/to/libmathx.so ./my-program
//
// The override names the object to load; that object still resolves its own
// dependencies through the dynamic loader's search path.
const LibraryPathEnv = "CPPGO_LIBRARY_PATH"

const (
	// cacheNamespace is the directory OpenFS owns inside the user's cache
	// directory. It is shared by every program using cppgo on the machine,
	// one subdirectory per embedded set.
	cacheNamespace = "cppgo"

	// cacheDirPerm and cacheFilePerm are the modes every cache entry has: a
	// directory nobody else can write and a file nobody else can read. A
	// shared object is mapped executable, so a file another local user can
	// replace would be arbitrary code in this process.
	cacheDirPerm  fs.FileMode = 0o700
	cacheFilePerm fs.FileMode = 0o600

	// sharedObjectSuffix is the only kind of file OpenFS extracts.
	sharedObjectSuffix = ".so"

	// tempPattern is the prefix os.CreateTemp fills in for the file that is
	// about to be renamed into place. CreateTemp creates it with O_EXCL and
	// 0600 inside the private cache directory, so the name is randomised and
	// can never be a world-writable path somebody prepared in advance.
	tempPattern = ".cppgo-extract-*"
)

// errFileChanged is what an extraction reports when the file system served
// different bytes when the object was copied than when it was hashed.
var errFileChanged = errors.New("file changed while it was being extracted")

// CacheDir returns the directory below which OpenFS extracts embedded shared
// objects: os.UserCacheDir()/cppgo. One subdirectory per embedded set lives
// there, named after the set's SHA-256.
//
// The path is a location, not a promise that anything is in it: OpenFS creates
// it on first use. It is a function rather than a constant because
// os.UserCacheDir reads the environment.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cppgo: locate the user cache directory: %w", err)
	}

	return filepath.Join(base, cacheNamespace), nil
}

// CleanCache removes every library OpenFS has extracted for the current user.
//
// It is the cache-clear helper a program runs to reclaim disk or to start from
// a known state: a later OpenFS extracts again from whatever it is given.
// Removing the cache while another goroutine or process is loading from it is
// not synchronised — an object already mapped stays mapped, and a process that
// extracts again recreates the entry — so a program calls it when it is the
// only user of the cache, normally at startup or shutdown.
func CleanCache() error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("cppgo: clear the library cache %q: %w", dir, err)
	}

	return nil
}

// OpenFS loads a shared object that is embedded in the program, materialising
// every shared object of fsys in a private cache first.
//
// fsys is any file system: an embed.FS produced by //go:embed is the intended
// source, and fstest.MapFS or os.DirFS serve the same purpose in tests and in
// development. name is the base name of the object to load and must be one of
// the ".so" entries of fsys.
//
// Every shared object in fsys is extracted, not only name, because the
// dependencies of an object are loaded by the dynamic loader and not by this
// function: the cppgo shim and the user's library are two files that have to
// sit beside each other, and a sibling is found through the DT_RUNPATH the
// loading object was linked with, conventionally
//
//	-Wl,-rpath,$ORIGIN
//
// so the loader looks in the directory the object itself was loaded from. A
// dependency that is not embedded — libc, libm, libstdc++, libgcc_s, or any
// system library the C++ side linked against — still comes from the loader's
// normal search path. Embedding makes the C++ side one file to ship; it does
// not make the Go binary static, and the binary is still dynamically linked to
// libc.
//
// Entries whose name ends in ".so" are extracted even when they live in a
// subdirectory, flattened into one directory because that is the only layout
// in which $ORIGIN sees them; two entries with one base name are an error
// rather than a silent last-one-wins. Other files are ignored.
//
// The cache is CacheDir()/<sha256>/<name>, where the digest covers the whole
// embedded set — every name, size and content — so a change to any of them
// lands in a new directory and an old directory is never combined with new
// contents. The directory is created 0700 and every file 0600; a file is
// written to a temporary file in that directory and renamed into place, so a
// reader never sees a partial object and concurrent goroutines or processes
// cannot corrupt an entry. An existing file is reused only when its size and
// SHA-256 match what was embedded, so a corrupted or truncated cache entry is
// replaced instead of loaded. The set is hashed on every call, which is what
// makes that check meaningful; the cost is one read of the embedded bytes.
//
// OpenFS is the deployment path: a program that embeds its C++ side needs no
// C++ build at run time and no library path in its environment. For a library
// the C++ build already produced, Open is the call. LibraryPathEnv overrides
// this function with an explicit path.
func OpenFS(fsys fs.FS, name string) (*Library, error) {
	if name == "" {
		return nil, &OpenError{Path: name, Err: errEmptyPath}
	}
	if fsys == nil {
		return nil, &OpenError{Path: name, Err: errNilFileSystem}
	}
	if override := os.Getenv(LibraryPathEnv); override != "" {
		return Open(override)
	}

	objects, err := scan(fsys)
	if err != nil {
		return nil, err
	}

	index := -1
	for i, o := range objects {
		if o.name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, &ExtractError{
			Name: name,
			Err:  fmt.Errorf("%w in the embedded file system", fs.ErrNotExist),
		}
	}

	dir, err := extract(fsys, objects)
	if err != nil {
		return nil, err
	}

	return Open(filepath.Join(dir, objects[index].name))
}

// object is one shared object found in an embedded file system.
type object struct {
	// name is the file name it takes in the cache directory.
	name string
	// source is where it lives in the file system.
	source string
	// size is its length in bytes.
	size int64
	// digest is the SHA-256 of its contents.
	digest [sha256.Size]byte
}

// scan lists the shared objects of fsys in a deterministic order.
//
// The order is by file name, not by directory order, so the digest of the set
// does not depend on how the file system happens to enumerate its entries.
func scan(fsys fs.FS) ([]object, error) {
	var (
		objects []object
		sources = map[string]string{}
	)

	err := fs.WalkDir(fsys, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		name := path.Base(p)
		if !strings.HasSuffix(name, sharedObjectSuffix) {
			return nil
		}
		if previous, duplicate := sources[name]; duplicate {
			return fmt.Errorf("%q and %q both extract to %q", previous, p, name)
		}

		size, digest, err := digestFS(fsys, p)
		if err != nil {
			return fmt.Errorf("read %q: %w", p, err)
		}

		sources[name] = p
		objects = append(objects, object{name: name, source: p, size: size, digest: digest})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cppgo: scan the embedded file system: %w", err)
	}

	sort.Slice(objects, func(i, j int) bool { return objects[i].name < objects[j].name })

	return objects, nil
}

// setDigest names the cache directory of an embedded set: a SHA-256 over the
// ordered triples of file name, size and content digest, so two programs that
// embed the same bytes share one entry and any change to any byte produces
// another.
func setDigest(objects []object) string {
	var buf []byte

	for _, o := range objects {
		buf = append(buf, o.name...)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, o.size, 10)
		buf = append(buf, 0)
		buf = append(buf, o.digest[:]...)
		buf = append(buf, '\n')
	}

	digest := sha256.Sum256(buf)

	return hex.EncodeToString(digest[:])
}

// extract materialises every object in a cache directory named after the set
// and returns it.
func extract(fsys fs.FS, objects []object) (string, error) {
	root, err := CacheDir()
	if err != nil {
		return "", err
	}
	if err := privateDir(root); err != nil {
		return "", err
	}

	dir := filepath.Join(root, setDigest(objects))
	if err := privateDir(dir); err != nil {
		return "", err
	}

	for _, o := range objects {
		if err := materialise(fsys, dir, o); err != nil {
			return "", err
		}
	}

	return dir, nil
}

// privateDir creates dir when it is absent and makes sure it is a directory
// this user owns exclusively, repairing a mode that time or another tool
// loosened.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, cacheDirPerm); err != nil {
		return fmt.Errorf("cppgo: create the cache directory %q: %w", dir, err)
	}

	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		// A symbolic link lands here: MkdirAll follows it, so the walk above
		// succeeded, and the entry is not a directory this user created.
		return fmt.Errorf("%w: %q", ErrInsecureCache, dir)
	}
	if info.Mode().Perm() != cacheDirPerm {
		if err := os.Chmod(dir, cacheDirPerm); err != nil {
			return fmt.Errorf("cppgo: make the cache directory %q private: %w", dir, err)
		}
	}

	return nil
}

// materialise writes o into dir unless a correct copy is already there.
func materialise(fsys fs.FS, dir string, o object) error {
	target := filepath.Join(dir, o.name)

	reuse, err := reusable(target, o)
	if err != nil {
		return err
	}
	if reuse {
		return nil
	}

	src, err := fsys.Open(o.source)
	if err != nil {
		return &ExtractError{Name: o.name, Dir: dir, Err: err}
	}

	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		_ = src.Close()
		return &ExtractError{Name: o.name, Dir: dir, Err: err}
	}
	tmpPath := tmp.Name()
	// The rename below consumes the temporary file; until then, every way out
	// of this function leaves the directory as it found it.
	defer func() { _ = os.Remove(tmpPath) }()

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), src)
	_ = src.Close()
	if err != nil {
		_ = tmp.Close()
		return &ExtractError{Name: o.name, Dir: dir, Err: err}
	}

	var digest [sha256.Size]byte
	h.Sum(digest[:0])
	if size != o.size || digest != o.digest {
		_ = tmp.Close()
		return &ExtractError{Name: o.name, Dir: dir, Err: errFileChanged}
	}

	if err := tmp.Chmod(cacheFilePerm); err != nil {
		_ = tmp.Close()
		return &ExtractError{Name: o.name, Dir: dir, Err: err}
	}
	if err := tmp.Close(); err != nil {
		return &ExtractError{Name: o.name, Dir: dir, Err: err}
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return &ExtractError{Name: o.name, Dir: dir, Err: err}
	}

	return nil
}

// reusable reports whether target already holds o, byte for byte, tightening a
// mode that is not private.
//
// Anything that is not a regular file of the right size is not reused: a
// symbolic link, a directory or a truncated copy is replaced by the atomic
// rename rather than followed or trusted.
func reusable(target string, o object) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, &ExtractError{Name: o.name, Dir: filepath.Dir(target), Err: err}
	}
	if !info.Mode().IsRegular() || info.Size() != o.size {
		return false, nil
	}
	if info.Mode().Perm() != cacheFilePerm {
		if err := os.Chmod(target, cacheFilePerm); err != nil {
			return false, &ExtractError{Name: o.name, Dir: filepath.Dir(target), Err: err}
		}
	}

	digest, err := digestPath(target)
	if err != nil {
		return false, &ExtractError{Name: o.name, Dir: filepath.Dir(target), Err: err}
	}

	return digest == o.digest, nil
}

// digestFS hashes the file name of fsys, counting the bytes read.
func digestFS(fsys fs.FS, name string) (int64, [sha256.Size]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return 0, [sha256.Size]byte{}, err
	}
	defer func() { _ = f.Close() }()

	return digestOf(f)
}

// digestPath hashes the file at path.
func digestPath(path string) ([sha256.Size]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer func() { _ = f.Close() }()

	_, digest, err := digestOf(f)
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	return digest, nil
}

// digestOf reads r to the end, returning its length and SHA-256.
func digestOf(r io.Reader) (int64, [sha256.Size]byte, error) {
	var digest [sha256.Size]byte

	h := sha256.New()
	size, err := io.Copy(h, r)
	if err != nil {
		return 0, digest, err
	}
	h.Sum(digest[:0])

	return size, digest, nil
}
