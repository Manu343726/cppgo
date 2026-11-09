package cppgo_test

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// The deployment fixture is two objects, which is the shape ADR-0002 gives a
// bound library: the cppgo shim and the user's library, each needing the other
// to be found beside it by the dynamic loader.
const (
	embeddedDepName  = "libcppgo_embed_dep.so"
	embeddedMainName = "libcppgo_embed_main.so"
	embeddedSymbol   = "cppgo_embed_main_add"

	embeddedDepSource = `
#include <cstdint>

// The sibling: main calls this, so a successful load proves the dynamic loader
// found the second object in the extraction directory.
extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_embed_dep_add(std::int64_t a, std::int64_t b) {
    return a + b;
}
`

	embeddedMainSource = `
#include <cstdint>

extern "C" std::int64_t cppgo_embed_dep_add(std::int64_t a, std::int64_t b);

// The main object: it calls the sibling, and doubles the result so a value of
// the wrong shape is visible.
extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_embed_main_add(std::int64_t a, std::int64_t b) {
    return 2 * cppgo_embed_dep_add(a, b);
}
`
)

// buildEmbedded compiles the fixture pair into dir and returns it.
//
// The main object is linked with -Wl,-rpath,$ORIGIN, which is the requirement
// this file exists to measure: runpathSearch below reads that entry back out
// of the ELF, so the test fails if the flag is dropped rather than failing
// somewhere inside dlopen.
func buildEmbedded(t *testing.T) string {
	t.Helper()

	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skipf("no g++ available to build the embedded fixture: %v", err)
	}

	dir := t.TempDir()

	depSource := filepath.Join(dir, "dep.cc")
	mainSource := filepath.Join(dir, "main.cc")
	require.NoError(t, os.WriteFile(depSource, []byte(embeddedDepSource), 0o644))
	require.NoError(t, os.WriteFile(mainSource, []byte(embeddedMainSource), 0o644))

	flags := []string{"-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17"}

	dep := filepath.Join(dir, embeddedDepName)
	runCompiler(t, gxx, append(append([]string{}, flags...), "-o", dep, depSource)...)

	main := filepath.Join(dir, embeddedMainName)
	runCompiler(t, gxx, append(append([]string{}, flags...),
		"-Wl,-rpath,$ORIGIN", "-o", main, mainSource, "-L"+dir, "-lcppgo_embed_dep")...)

	return dir
}

// runCompiler runs one g++ invocation and fails the test with its output.
func runCompiler(t *testing.T, compiler string, args ...string) {
	t.Helper()

	output, err := exec.Command(compiler, args...).CombinedOutput()
	require.NoErrorf(t, err, "%s %s:\n%s", compiler, strings.Join(args, " "), output)
}

// embeddedObjects reads the built fixture into a MapFS, so a test can serve it
// as though it had been embedded.
func embeddedObjects(t *testing.T, dir string) fstest.MapFS {
	t.Helper()

	return fstest.MapFS{
		embeddedDepName: &fstest.MapFile{
			Data: readBytes(t, filepath.Join(dir, embeddedDepName)),
			Mode: 0o644,
		},
		embeddedMainName: &fstest.MapFile{
			Data: readBytes(t, filepath.Join(dir, embeddedMainName)),
			Mode: 0o644,
		},
	}
}

// isolateCache points os.UserCacheDir at a fresh directory for the test, and
// clears the explicit-path override so no test inherits it from the shell.
//
// os.UserCacheDir reads XDG_CACHE_HOME on every supported Unix, so this is the
// same switch a user's environment has: no hook exists in the package, and
// none is needed.
func isolateCache(t *testing.T) string {
	t.Helper()

	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	t.Setenv(cppgo.LibraryPathEnv, "")

	return base
}

// cacheRoot is the directory cppgo owns inside an isolated cache base.
func cacheRoot(base string) string { return filepath.Join(base, "cppgo") }

// readBytes reads a file the test just built.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return data
}

// openEmbedded loads the fixture through OpenFS and calls it, so a returned
// Library is one whose main object and sibling were both resolved.
func openEmbedded(t *testing.T, fsys fs.FS) *cppgo.Library {
	t.Helper()

	lib, err := cppgo.OpenFS(fsys, embeddedMainName)
	require.NoError(t, err)
	require.NotNil(t, lib)
	t.Cleanup(func() { _ = lib.Close() })

	require.EqualValues(t, 10, callInt(t, fn(t, lib, embeddedSymbol), [6]uintptr{2, 3}))

	return lib
}

// callEmbedded makes the same call without a testing.T, for goroutines.
func callEmbedded(lib *cppgo.Library) (int64, error) {
	addr, err := lib.Symbol(embeddedSymbol)
	if err != nil {
		return 0, err
	}

	cppgo.ProbeStack()
	raw, _ := asm.Call(rtl.Pointer(addr), &asm.Args{I: [6]uintptr{2, 3}})

	return int64(raw), nil
}

// runpathSearch returns the DT_RUNPATH and DT_RPATH entries of the ELF file.
func runpathSearch(t *testing.T, data []byte) []string {
	t.Helper()

	file, err := elf.NewFile(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	var search []string

	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		values, err := file.DynString(tag)
		if err != nil {
			continue
		}
		search = append(search, values...)
	}

	return search
}

// neededObjects returns the DT_NEEDED entries of the ELF file.
func neededObjects(t *testing.T, data []byte) []string {
	t.Helper()

	file, err := elf.NewFile(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	needed, err := file.DynString(elf.DT_NEEDED)
	require.NoError(t, err)

	return needed
}

func TestOpenFSEmbedded(t *testing.T) {
	dir := buildEmbedded(t)
	base := isolateCache(t)
	objects := embeddedObjects(t, dir)

	t.Run("sibling resolves through $ORIGIN", func(t *testing.T) {
		// The two claims the loader depends on, measured on the fixture
		// itself: main names the sibling, and the search path is the
		// directory the object was loaded from.
		search := runpathSearch(t, objects[embeddedMainName].Data)
		require.NotEmpty(t, search, "the fixture must carry a run path")
		assert.Contains(t, search, "$ORIGIN")

		assert.Contains(t, neededObjects(t, objects[embeddedMainName].Data), embeddedDepName)
	})

	t.Run("map file system", func(t *testing.T) {
		lib := openEmbedded(t, objects)

		entry := filepath.Dir(lib.Path())
		assert.Equal(t, cacheRoot(base), filepath.Dir(entry))
		assert.Len(t, filepath.Base(entry), 64, "the cache entry is named by a SHA-256")
		assert.Equal(t, filepath.Join(entry, embeddedMainName), lib.Path())
	})

	t.Run("directory file system", func(t *testing.T) {
		lib := openEmbedded(t, os.DirFS(dir))
		assert.Equal(t, cacheRoot(base), filepath.Dir(filepath.Dir(lib.Path())))
	})

	assertCacheLayout(t, base)
}

// assertCacheLayout asserts the permissions the cache contract states, on the
// real directories rather than on a mock: 0700 for directories, 0600 for
// objects, one entry for one embedded set, and no temporary file left behind.
func assertCacheLayout(t *testing.T, base string) {
	t.Helper()

	root := cacheRoot(base)
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "cache root %q", root)

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "one cache entry for one embedded set")

	entry := filepath.Join(root, entries[0].Name())
	info, err = os.Stat(entry)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "cache entry %q", entry)

	names := make([]string, 0, 2)
	for _, name := range []string{embeddedDepName, embeddedMainName} {
		info, err := os.Stat(filepath.Join(entry, name))
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular(), "%s must be a regular file", name)
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "%s", name)
		names = append(names, name)
	}
	assert.Equal(t, []string{embeddedDepName, embeddedMainName}, names)
}

func TestOpenFSReusesCache(t *testing.T) {
	dir := buildEmbedded(t)
	isolateCache(t)
	objects := embeddedObjects(t, dir)

	first := openEmbedded(t, objects)
	entry := filepath.Dir(first.Path())

	// A stamp in the past: an extraction that rewrites the file cannot
	// produce this mtime, because a rewrite renames a freshly created file
	// into place.
	stamp := time.Unix(1_600_000_000, 0)
	for _, name := range []string{embeddedDepName, embeddedMainName} {
		require.NoError(t, os.Chtimes(filepath.Join(entry, name), stamp, stamp))
	}

	second := openEmbedded(t, objects)
	require.Equal(t, first.Path(), second.Path())

	for _, name := range []string{embeddedDepName, embeddedMainName} {
		info, err := os.Stat(filepath.Join(entry, name))
		require.NoError(t, err)
		assert.True(t, info.ModTime().Equal(stamp), "%s was rewritten on the second run", name)
	}
}

func TestOpenFSRepairsCache(t *testing.T) {
	dir := buildEmbedded(t)
	isolateCache(t)
	objects := embeddedObjects(t, dir)

	lib := openEmbedded(t, objects)
	entry := filepath.Dir(lib.Path())
	main := filepath.Join(entry, embeddedMainName)
	dep := filepath.Join(entry, embeddedDepName)

	original := readBytes(t, main)
	depOriginal := readBytes(t, dep)

	// The object is mapped while it is open, so a test that damages it has to
	// unload it first: writing to a mapped file corrupts the running code
	// before it corrupts the cache.
	require.NoError(t, lib.Close())

	t.Run("same-size corruption is replaced", func(t *testing.T) {
		require.NoError(t, os.WriteFile(main, bytes.Repeat([]byte{0x90}, len(original)), 0o600))

		closed := openEmbedded(t, objects)
		require.NoError(t, closed.Close())
		assert.Equal(t, original, readBytes(t, main))
	})

	t.Run("truncated dependency is replaced", func(t *testing.T) {
		require.NoError(t, os.WriteFile(dep, depOriginal[:16], 0o600))

		closed := openEmbedded(t, objects)
		require.NoError(t, closed.Close())
		assert.Equal(t, depOriginal, readBytes(t, dep))
	})

	t.Run("loose file mode is tightened without a rewrite", func(t *testing.T) {
		require.NoError(t, os.Chmod(main, 0o644))

		stamp := time.Unix(1_600_000_001, 0)
		require.NoError(t, os.Chtimes(main, stamp, stamp))

		closed := openEmbedded(t, objects)
		require.NoError(t, closed.Close())

		info, err := os.Stat(main)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
		assert.True(t, info.ModTime().Equal(stamp), "a correct file must be reused, not rewritten")
	})

	t.Run("loose directory mode is tightened", func(t *testing.T) {
		root := filepath.Dir(entry)
		require.NoError(t, os.Chmod(entry, 0o755))
		require.NoError(t, os.Chmod(root, 0o755))

		closed := openEmbedded(t, objects)
		require.NoError(t, closed.Close())

		for _, path := range []string{entry, root} {
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "%q", path)
		}
	})
}

func TestOpenFSRejectsInsecureCache(t *testing.T) {
	dir := buildEmbedded(t)
	isolateCache(t)
	objects := embeddedObjects(t, dir)

	lib := openEmbedded(t, objects)
	entry := filepath.Dir(lib.Path())
	require.NoError(t, os.RemoveAll(entry))

	// A directory at the entry path that is reachable, but not one OpenFS
	// owns: MkdirAll follows it, Lstat sees the link.
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, entry))

	loaded, err := cppgo.OpenFS(objects, embeddedMainName)
	require.Error(t, err)
	assert.Nil(t, loaded)
	assert.ErrorIs(t, err, cppgo.ErrInsecureCache)
	assert.Contains(t, err.Error(), entry)
}

func TestOpenFSMissingEntry(t *testing.T) {
	base := isolateCache(t)

	fsys := fstest.MapFS{"libcppgo_present.so": &fstest.MapFile{Data: []byte("not really")}}

	lib, err := cppgo.OpenFS(fsys, "libcppgo_absent.so")
	require.Error(t, err)
	assert.Nil(t, lib)

	var extractErr *cppgo.ExtractError
	require.ErrorAs(t, err, &extractErr)
	assert.Equal(t, "libcppgo_absent.so", extractErr.Name)
	assert.Empty(t, extractErr.Dir)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), "libcppgo_absent.so")

	// A name the file system does not have is reported before anything is
	// written: no cache entry is created for a typo.
	_, statErr := os.Stat(cacheRoot(base))
	assert.ErrorIs(t, statErr, fs.ErrNotExist)
}

func TestOpenFSArguments(t *testing.T) {
	tests := []struct {
		name string
		fsys fs.FS
		lib  string
		want string
	}{
		{name: "empty name", fsys: fstest.MapFS{}, lib: "", want: "empty library path"},
		{name: "nil file system", fsys: nil, lib: "libx.so", want: "nil file system"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateCache(t)

			lib, err := cppgo.OpenFS(test.fsys, test.lib)
			require.Error(t, err)
			assert.Nil(t, lib)
			assert.Contains(t, err.Error(), test.want)

			var openErr *cppgo.OpenError
			require.ErrorAs(t, err, &openErr)
			assert.Equal(t, test.lib, openErr.Path)
		})
	}
}

// statErrorFS fails the walk OpenFS starts with.
type statErrorFS struct{ err error }

func (f statErrorFS) Open(string) (fs.File, error) { return nil, f.err }

// failingReadFS serves base, but every read of name fails.
type failingReadFS struct {
	fs.FS
	name string
	err  error
}

func (f failingReadFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil || name != f.name {
		return file, err
	}

	return failingReadFile{File: file, err: f.err}, nil
}

// failingReadFile is an fs.File whose Read always fails.
type failingReadFile struct {
	fs.File
	err error
}

func (f failingReadFile) Read([]byte) (int, error) { return 0, f.err }

// openFailsFS fails every open of name, which is the scan's own open.
type openFailsFS struct {
	fs.FS
	name string
	err  error
}

func (f openFailsFS) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, f.err
	}

	return f.FS.Open(name)
}

// secondReadFailsFS serves base normally to the scan and fails the copy: the
// second open succeeds, and reading it does not.
type secondReadFailsFS struct {
	fs.FS
	name string
	err  error

	mu    sync.Mutex
	opens int
}

func (f *secondReadFailsFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil || name != f.name {
		return file, err
	}

	f.mu.Lock()
	f.opens++
	opens := f.opens
	f.mu.Unlock()

	if opens == 1 {
		return file, nil
	}

	return failingReadFile{File: file, err: f.err}, nil
}

// secondOpenFS fails every open of name after the first, which is how a file
// that was hashed cannot be read again.
type secondOpenFS struct {
	fs.FS
	name string
	err  error

	mu    sync.Mutex
	opens int
}

func (f *secondOpenFS) Open(name string) (fs.File, error) {
	if name == f.name {
		f.mu.Lock()
		f.opens++
		opens := f.opens
		f.mu.Unlock()

		if opens > 1 {
			return nil, f.err
		}
	}

	return f.FS.Open(name)
}

// changedContentFS serves different bytes for name from the second open on.
type changedContentFS struct {
	fs.FS
	name        string
	replacement []byte

	mu    sync.Mutex
	opens int
}

func (f *changedContentFS) Open(name string) (fs.File, error) {
	if name == f.name {
		f.mu.Lock()
		f.opens++
		opens := f.opens
		f.mu.Unlock()

		if opens > 1 {
			replacement := fstest.MapFS{
				name: &fstest.MapFile{Data: f.replacement, Mode: 0o644},
			}
			return replacement.Open(name)
		}
	}

	return f.FS.Open(name)
}

func TestOpenFSBadFileSystem(t *testing.T) {
	dir := buildEmbedded(t)
	isolateCache(t)
	objects := embeddedObjects(t, dir)

	boom := errors.New("boom")

	tests := []struct {
		name string
		fsys fs.FS
		lib  string
		want string
	}{
		{
			name: "walk fails",
			fsys: statErrorFS{err: boom},
			lib:  embeddedMainName,
			want: "scan the embedded file system",
		},
		{
			name: "duplicate base name",
			fsys: fstest.MapFS{
				"a/libcppgo_embed_dep.so": &fstest.MapFile{Data: objects[embeddedDepName].Data},
				"b/libcppgo_embed_dep.so": &fstest.MapFile{Data: objects[embeddedDepName].Data},
			},
			lib:  embeddedDepName,
			want: "both extract to",
		},
		{
			name: "read fails while hashing",
			fsys: failingReadFS{FS: objects, name: embeddedMainName, err: boom},
			lib:  embeddedMainName,
			want: "read",
		},
		{
			name: "open fails while hashing",
			fsys: openFailsFS{FS: objects, name: embeddedMainName, err: boom},
			lib:  embeddedMainName,
			want: "boom",
		},
		{
			name: "open fails while copying",
			fsys: &secondOpenFS{FS: objects, name: embeddedDepName, err: boom},
			lib:  embeddedMainName,
			want: embeddedDepName,
		},
		{
			name: "read fails while copying",
			fsys: &secondReadFailsFS{FS: objects, name: embeddedDepName, err: boom},
			lib:  embeddedMainName,
			want: "boom",
		},
		{
			name: "contents change between hash and copy",
			fsys: &changedContentFS{
				FS:          objects,
				name:        embeddedDepName,
				replacement: []byte("different, and shorter"),
			},
			lib:  embeddedMainName,
			want: "file changed while it was being extracted",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lib, err := cppgo.OpenFS(test.fsys, test.lib)
			require.Error(t, err)
			assert.Nil(t, lib)
			assert.Contains(t, err.Error(), test.want)
		})
	}

	t.Run("a failed copy leaves no target and no temporary file", func(t *testing.T) {
		base := isolateCache(t)

		_, err := cppgo.OpenFS(
			&changedContentFS{FS: objects, name: embeddedDepName, replacement: []byte("different")},
			embeddedMainName,
		)
		require.Error(t, err)

		entries, readErr := os.ReadDir(cacheRoot(base))
		require.NoError(t, readErr)
		for _, entry := range entries {
			names, readErr := os.ReadDir(filepath.Join(cacheRoot(base), entry.Name()))
			require.NoError(t, readErr)
			for _, name := range names {
				assert.NotContains(t, name.Name(), ".cppgo-extract-")
				assert.NotEqual(t, embeddedDepName, name.Name(), "a failed object must not be published")
			}
		}
	})
}

func TestOpenFSNotSharedObject(t *testing.T) {
	base := isolateCache(t)

	fsys := fstest.MapFS{
		"libcppgo_broken.so": &fstest.MapFile{Data: []byte("this is not an ELF file\n"), Mode: 0o644},
	}

	lib, err := cppgo.OpenFS(fsys, "libcppgo_broken.so")
	require.Error(t, err)
	assert.Nil(t, lib)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, "libcppgo_broken.so", filepath.Base(openErr.Path))
	assert.Contains(t, err.Error(), "libcppgo_broken.so")

	// The bytes are a cache entry even though they are not loadable: the
	// extraction is what failed to be a library, not the file system.
	entry := filepath.Dir(openErr.Path)
	assert.Equal(t, cacheRoot(base), filepath.Dir(entry))
	assert.Len(t, filepath.Base(entry), 64, "the cache entry is named by a SHA-256")

	info, statErr := os.Stat(openErr.Path)
	require.NoError(t, statErr)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}

func TestOpenFSCacheFailure(t *testing.T) {
	dir := buildEmbedded(t)
	objects := embeddedObjects(t, dir)

	t.Run("cache root is a regular file", func(t *testing.T) {
		base := isolateCache(t)
		require.NoError(t, os.WriteFile(cacheRoot(base), []byte("not a directory"), 0o600))

		lib, err := cppgo.OpenFS(objects, embeddedMainName)
		require.Error(t, err)
		assert.Nil(t, lib)

		// A file where a private directory belongs is the sentinel's shape,
		// not a generic creation failure: the caller can tell "refused" from
		// "could not create" with errors.Is.
		assert.ErrorIs(t, err, cppgo.ErrInsecureCache)
		assert.Contains(t, err.Error(), cacheRoot(base))
		// The underlying file-system error stays in the chain.
		assert.Contains(t, err.Error(), "not a directory")
	})

	t.Run("cache entry is a regular file", func(t *testing.T) {
		isolateCache(t)
		loaded := openEmbedded(t, objects)
		entry := filepath.Dir(loaded.Path())
		require.NoError(t, loaded.Close())

		require.NoError(t, os.RemoveAll(entry))
		require.NoError(t, os.WriteFile(entry, []byte("not a directory"), 0o600))

		_, err := cppgo.OpenFS(objects, embeddedMainName)
		require.Error(t, err)
		assert.ErrorIs(t, err, cppgo.ErrInsecureCache)
		assert.Contains(t, err.Error(), entry)
	})

	t.Run("uncreatable cache is not an insecure path", func(t *testing.T) {
		// The other side of the sentinel's line: a cache root whose parent is
		// a file is a creation failure, not a path OpenFS refuses to use.
		base := filepath.Join(t.TempDir(), "cache-is-a-file")
		require.NoError(t, os.WriteFile(base, []byte("not a directory"), 0o600))
		t.Setenv("XDG_CACHE_HOME", base)
		t.Setenv(cppgo.LibraryPathEnv, "")

		_, err := cppgo.OpenFS(objects, embeddedMainName)
		require.Error(t, err)
		assert.NotErrorIs(t, err, cppgo.ErrInsecureCache)
		assert.Contains(t, err.Error(), "create the cache directory")
	})

	t.Run("rename fails over a directory", func(t *testing.T) {
		isolateCache(t)
		loaded := openEmbedded(t, objects)
		path := loaded.Path()
		require.NoError(t, loaded.Close())

		// A directory cannot be replaced by the rename, so the object is
		// refused with an error naming it rather than silently served from
		// the wrong shape.
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.Mkdir(path, 0o700))

		_, err := cppgo.OpenFS(objects, embeddedMainName)
		require.Error(t, err)

		var extractErr *cppgo.ExtractError
		require.ErrorAs(t, err, &extractErr)
		assert.Equal(t, embeddedMainName, extractErr.Name)
		assert.Equal(t, filepath.Dir(path), extractErr.Dir)

		entries, readErr := os.ReadDir(filepath.Dir(path))
		require.NoError(t, readErr)
		for _, entry := range entries {
			assert.NotContains(t, entry.Name(), ".cppgo-extract-")
		}
	})
}

func TestOpenFSExtractsEverySharedObject(t *testing.T) {
	dir := buildEmbedded(t)
	isolateCache(t)
	objects := embeddedObjects(t, dir)

	fsys := fstest.MapFS{
		// The sibling lives in a subdirectory of the file system and is
		// flattened into the cache, because that is the only layout in which
		// $ORIGIN can see it.
		"objects/" + embeddedDepName: &fstest.MapFile{Data: objects[embeddedDepName].Data, Mode: 0o644},
		embeddedMainName:             &fstest.MapFile{Data: objects[embeddedMainName].Data, Mode: 0o644},
		"README.md":                  &fstest.MapFile{Data: []byte("not a library"), Mode: 0o644},
		"data.bin":                   &fstest.MapFile{Data: []byte("also not a library"), Mode: 0o644},
	}

	lib := openEmbedded(t, fsys)
	entry := filepath.Dir(lib.Path())

	for _, name := range []string{embeddedDepName, embeddedMainName} {
		assert.FileExists(t, filepath.Join(entry, name))
	}
	assert.NoFileExists(t, filepath.Join(entry, "README.md"))
	assert.NoFileExists(t, filepath.Join(entry, "data.bin"))
}

func TestOpenFSDoesNotUseSystemTemp(t *testing.T) {
	dir := buildEmbedded(t)
	isolateCache(t)
	objects := embeddedObjects(t, dir)

	// If extraction ever stages through os.TempDir, this test cannot pass:
	// the directory does not exist.
	unusable := filepath.Join(t.TempDir(), "absent")
	t.Setenv("TMPDIR", unusable)

	openEmbedded(t, objects)
}

func TestOpenFSConcurrentGoroutines(t *testing.T) {
	dir := buildEmbedded(t)
	base := isolateCache(t)
	objects := embeddedObjects(t, dir)

	const goroutines = 32

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)
	paths := make(chan string, goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			lib, err := cppgo.OpenFS(objects, embeddedMainName)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = lib.Close() }()

			got, err := callEmbedded(lib)
			if err != nil {
				errs <- err
				return
			}
			if got != 10 {
				errs <- fmt.Errorf("cppgo_embed_main_add(2, 3) = %d, want 10", got)
				return
			}

			paths <- lib.Path()
		}()
	}

	wg.Wait()
	close(errs)
	close(paths)

	for err := range errs {
		require.NoError(t, err)
	}

	var first string
	for path := range paths {
		if first == "" {
			first = path
		}
		assert.Equal(t, first, path, "every goroutine must see one cache entry")
	}
	require.NotEmpty(t, first)

	assertCacheLayout(t, base)
}

func TestOpenFSConcurrentProcesses(t *testing.T) {
	dir := buildEmbedded(t)
	base := isolateCache(t)

	const processes = 4

	var wg sync.WaitGroup
	wg.Add(processes)

	outputs := make([]string, processes)
	errs := make([]error, processes)

	for i := range processes {
		go func() {
			defer wg.Done()

			cmd := exec.Command(os.Args[0], "-test.run=^TestOpenFSHelperProcess$")
			cmd.Env = append(os.Environ(),
				"CPPGO_EMBED_HELPER=1",
				"CPPGO_EMBED_FIXTURE="+dir,
				"XDG_CACHE_HOME="+base,
				cppgo.LibraryPathEnv+"=",
			)

			output, err := cmd.CombinedOutput()
			outputs[i] = string(output)
			errs[i] = err
		}()
	}

	wg.Wait()

	paths := make([]string, 0, processes)
	for i := range processes {
		require.NoErrorf(t, errs[i], "process %d:\n%s", i, outputs[i])

		path := ""
		for _, line := range strings.Split(outputs[i], "\n") {
			if value, found := strings.CutPrefix(line, "cppgo path="); found {
				path = strings.TrimSpace(value)
			}
		}
		require.NotEmptyf(t, path, "process %d did not report a library path:\n%s", i, outputs[i])
		paths = append(paths, path)
	}

	for _, path := range paths {
		assert.Equal(t, paths[0], path, "every process must see one cache entry")
	}

	assertCacheLayout(t, base)
}

// TestOpenFSHelperProcess is the body of a child process started by
// TestOpenFSConcurrentProcesses; it skips unless it is that child.
func TestOpenFSHelperProcess(t *testing.T) {
	if os.Getenv("CPPGO_EMBED_HELPER") != "1" {
		t.Skip("re-executed by TestOpenFSConcurrentProcesses")
	}

	dir := os.Getenv("CPPGO_EMBED_FIXTURE")

	lib, err := cppgo.OpenFS(os.DirFS(dir), embeddedMainName)
	require.NoError(t, err)

	require.EqualValues(t, 10, callInt(t, fn(t, lib, embeddedSymbol), [6]uintptr{2, 3}))

	fmt.Fprintf(os.Stdout, "cppgo path=%s\n", lib.Path())

	require.NoError(t, lib.Close())
}

func TestOpenFSLibraryPathOverride(t *testing.T) {
	isolateCache(t)
	explicit := testlib.Build(t, testlib.Full)
	t.Setenv(cppgo.LibraryPathEnv, explicit)

	// The file system is empty on purpose: an override must not need one.
	lib, err := cppgo.OpenFS(fstest.MapFS{}, "libdoes_not_exist.so")
	require.NoError(t, err)
	defer func() { _ = lib.Close() }()

	assert.Equal(t, explicit, lib.Path())
	assert.EqualValues(t, 5, callInt(t, fn(t, lib, "cppgo_fixture_add"), [6]uintptr{2, 3}))

	t.Run("a stale override falls through to the set", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "libcppgo_absent.so")
		t.Setenv(cppgo.LibraryPathEnv, missing)

		dir := buildEmbedded(t)
		base := isolateCache(t)
		lib, err := cppgo.OpenFS(embeddedObjects(t, dir), embeddedMainName)
		require.NoError(t, err)
		defer func() { _ = lib.Close() }()

		assert.Equal(t, cacheRoot(base), filepath.Dir(filepath.Dir(lib.Path())),
			"the embedded set is the deployment, not the override that found nothing")
	})
}

func TestCleanCache(t *testing.T) {
	dir := buildEmbedded(t)
	base := isolateCache(t)
	objects := embeddedObjects(t, dir)

	lib := openEmbedded(t, objects)
	require.DirExists(t, cacheRoot(base))
	require.FileExists(t, lib.Path())

	require.NoError(t, cppgo.CleanCache())

	_, err := os.Stat(cacheRoot(base))
	assert.ErrorIs(t, err, fs.ErrNotExist)

	// Clearing an empty cache is not an error, and the cache is derived
	// state: the next call extracts the set again.
	require.NoError(t, cppgo.CleanCache())

	again := openEmbedded(t, objects)
	assert.FileExists(t, again.Path())
}

func TestCleanCacheFailure(t *testing.T) {
	// A file where the cache root belongs: nothing below it can be removed,
	// whatever the caller owns.
	base := filepath.Join(t.TempDir(), "cache-is-a-file")
	require.NoError(t, os.WriteFile(base, []byte("not a directory"), 0o600))
	t.Setenv("XDG_CACHE_HOME", base)

	err := cppgo.CleanCache()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clear the library cache")
	assert.Contains(t, err.Error(), cacheRoot(base))
}

func TestCacheDirUnavailable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv(cppgo.LibraryPathEnv, "")

	_, err := cppgo.CacheDir()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locate the user cache directory")

	assert.Error(t, cppgo.CleanCache())

	fsys := fstest.MapFS{"libx.so": &fstest.MapFile{Data: []byte("not really"), Mode: 0o644}}

	_, err = cppgo.OpenFS(fsys, "libx.so")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locate the user cache directory")
}
