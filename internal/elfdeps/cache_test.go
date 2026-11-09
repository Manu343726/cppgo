package elfdeps_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/elfdeps"
)

// cacheImage builds a glibc ld.so.cache image: the new header, the entry array
// padded to the alignment of the entry's 64-bit hwcap, and one NUL-terminated
// string per key and value. Offsets are counted from the header, which is what
// the loader does.
func cacheImage(entries []cachedPair) []byte {
	const (
		magicSize  = 20
		headerSize = 48
		entrySize  = 24
	)

	stringsStart := headerSize + entrySize*len(entries)

	var table []byte
	offsets := make([]uint32, 0, 2*len(entries))
	for _, entry := range entries {
		for _, value := range []string{entry.name, entry.path} {
			offsets = append(offsets, uint32(stringsStart+len(table)))
			table = append(table, value...)
			table = append(table, 0)
		}
	}

	image := make([]byte, stringsStart, stringsStart+len(table))
	copy(image, "glibc-ld.so.cache1.1")
	binary.NativeEndian.PutUint32(image[magicSize:], uint32(len(entries)))
	binary.NativeEndian.PutUint32(image[magicSize+4:], uint32(len(table)))
	binary.NativeEndian.PutUint32(image[magicSize+8:], 0)

	for i := range entries {
		entry := headerSize + i*entrySize
		binary.NativeEndian.PutUint32(image[entry+4:], offsets[2*i])
		binary.NativeEndian.PutUint32(image[entry+8:], offsets[2*i+1])
	}

	return append(image, table...)
}

// cachedPair is one name and the file a cache image maps it to.
type cachedPair struct {
	name string
	path string
}

// writeCache writes a cache image into a temporary directory and returns its
// path.
func writeCache(t *testing.T, image []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ld.so.cache")
	require.NoError(t, os.WriteFile(path, image, 0o644))

	return path
}

func TestResolveFromCache(t *testing.T) {
	_, dep, user := buildPair(t)
	obj := requester(t, user)

	cache := writeCache(t, cacheImage([]cachedPair{
		{name: depSOName, path: dep},
	}))

	path, ok := elfdeps.Resolver{
		LibraryPath: []string{},
		Cache:       cache,
		DefaultDirs: []string{},
	}.Resolve(obj, depSOName)
	require.True(t, ok)
	assert.Equal(t, dep, path)
}

func TestResolveCachePrefersTheRightClass(t *testing.T) {
	_, dep, user := buildPair(t)
	obj := requester(t, user)

	// The first entry names a file that is not a shared object at all, which is
	// what a cache entry for the other ELF class looks like from here: names
	// match, the object does not.
	text := filepath.Join(t.TempDir(), "not-an-object.so")
	require.NoError(t, os.WriteFile(text, []byte("not an ELF file\n"), 0o644))

	cache := writeCache(t, cacheImage([]cachedPair{
		{name: depSOName, path: text},
		{name: depSOName, path: dep},
	}))

	path, ok := elfdeps.Resolver{
		LibraryPath: []string{},
		Cache:       cache,
		DefaultDirs: []string{},
	}.Resolve(obj, depSOName)
	require.True(t, ok)
	assert.Equal(t, dep, path, "the entry that names a real object of this class wins")
}

func TestResolveCacheIgnoresWhatItCannotUse(t *testing.T) {
	_, _, user := buildPair(t)
	obj := requester(t, user)

	t.Run("no cache file", func(t *testing.T) {
		_, ok := elfdeps.Resolver{
			LibraryPath: []string{},
			Cache:       filepath.Join(t.TempDir(), "absent"),
			DefaultDirs: []string{},
		}.Resolve(obj, depSOName)
		assert.False(t, ok)
	})

	t.Run("a cache that is not one", func(t *testing.T) {
		cache := writeCache(t, []byte("not a cache at all"))

		_, ok := elfdeps.Resolver{
			LibraryPath: []string{},
			Cache:       cache,
			DefaultDirs: []string{},
		}.Resolve(obj, depSOName)
		assert.False(t, ok)
	})

	t.Run("a truncated entry table", func(t *testing.T) {
		image := cacheImage([]cachedPair{{name: depSOName, path: "/nowhere"}})
		cache := writeCache(t, image[:60])

		_, ok := elfdeps.Resolver{
			LibraryPath: []string{},
			Cache:       cache,
			DefaultDirs: []string{},
		}.Resolve(obj, depSOName)
		assert.False(t, ok)
	})
}

// TestResolveFromSystemCache measures the parse against the cache the running
// system's loader uses, which is the only way to know the format is right.
func TestResolveFromSystemCache(t *testing.T) {
	if _, err := os.Stat(elfdeps.SystemCache); err != nil {
		t.Skipf("no %s on this system: %v", elfdeps.SystemCache, err)
	}

	_, _, user := buildPair(t)
	obj := requester(t, user)

	path, ok := elfdeps.Resolver{
		LibraryPath: []string{},
		DefaultDirs: []string{},
	}.Resolve(obj, "libc.so.6")
	require.True(t, ok, "libc.so.6 must resolve through %s", elfdeps.SystemCache)
	assert.FileExists(t, path)
	assert.Contains(t, path, "libc.so.6")
}

func TestSystemDefaultDirs(t *testing.T) {
	dirs := elfdeps.SystemDefaultDirs()

	assert.NotEmpty(t, dirs)
	assert.Contains(t, dirs, "/lib")
	assert.Contains(t, dirs, "/usr/lib")
}

// patchOffset rewrites one 32-bit field of an entry in a cache image: 0 is the
// flags word, 4 the key offset and 8 the path offset.
func patchOffset(image []byte, entry, field int, value uint32) {
	binary.NativeEndian.PutUint32(image[48+entry*24+field:], value)
}

// TestCacheEntryBounds covers the entries a corrupt image can hold: an offset
// outside the image and a string that is not terminated.
func TestCacheEntryBounds(t *testing.T) {
	_, dep, user := buildPair(t)
	obj := requester(t, user)

	resolverFor := func(image []byte) elfdeps.Resolver {
		return elfdeps.Resolver{
			LibraryPath: []string{},
			Cache:       writeCache(t, image),
			DefaultDirs: []string{},
		}
	}

	t.Run("key outside the image", func(t *testing.T) {
		image := cacheImage([]cachedPair{{name: depSOName, path: dep}})
		patchOffset(image, 0, 4, uint32(len(image)))

		_, ok := resolverFor(image).Resolve(obj, depSOName)
		assert.False(t, ok)
	})

	t.Run("path outside the image", func(t *testing.T) {
		image := cacheImage([]cachedPair{{name: depSOName, path: dep}})
		patchOffset(image, 0, 8, uint32(len(image)))

		_, ok := resolverFor(image).Resolve(obj, depSOName)
		assert.False(t, ok)
	})

	t.Run("a string with no terminator", func(t *testing.T) {
		image := cacheImage([]cachedPair{{name: depSOName, path: dep}})
		// The last byte of the image is the terminator of the last string;
		// dropping it leaves a key that never ends.
		image = image[:len(image)-1]
		patchOffset(image, 0, 4, uint32(len(image)-1))

		_, ok := resolverFor(image).Resolve(obj, depSOName)
		assert.False(t, ok)
	})
}
