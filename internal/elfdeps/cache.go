package elfdeps

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"unsafe"
)

const (
	// cacheMagic is the header of the cache format ldconfig writes today.
	cacheMagic = "glibc-ld.so.cache1.1"

	// cacheHeader is the fixed part of a cache header: the magic, the entry
	// count, the string-table length, the flags and three reserved words.
	cacheHeader = len(cacheMagic) + 4 + 4 + 4 + 12

	// cacheEntrySize is the size of one entry on every architecture: a 32-bit
	// flags word, three 32-bit offsets and the 64-bit hwcap.
	cacheEntrySize = 24
)

// cacheEntryOffset is where the entry array starts: the header padded to the
// alignment of the entry's 64-bit hwcap, which is 48 bytes on a 64-bit host.
var cacheEntryOffset = int(unsafe.Offsetof(struct {
	header [cacheHeader]byte
	hwcap  uint64
}{}.hwcap))

// cachedLibrary is one name the loader's cache maps to a file.
type cachedLibrary struct {
	// name is the soname or file name the entry answers to.
	name string
	// path is the file it resolves to.
	path string
}

// lookupCache finds name in the loader's cache.
//
// The cache is read once per name. It is a few hundred kilobytes and only the
// names that no run path and no LD_LIBRARY_PATH entry resolved reach it, so the
// read is not worth a cache of its own.
func (r Resolver) lookupCache(name string, class elf.Class) (string, bool) {
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		return "", false
	}

	for _, entry := range parseCache(data) {
		if entry.name != name || entry.path == "" {
			continue
		}
		// A multiarch cache holds the 32-bit and the 64-bit copy of a name;
		// the loader must not take the other word size.
		if !sameClass(entry.path, class) {
			continue
		}

		return entry.path, true
	}

	return "", false
}

// parseCache reads the entries of a glibc ld.so.cache image.
//
// ldconfig writes a compatibility header first and then, after it, the current
// one; a modern ldconfig writes the current header only. The magic search covers
// both without reconstructing either header, and the offsets are taken relative
// to the header that was found. A corrupt image is an empty cache rather than an
// error, because the loader ignores one too.
func parseCache(data []byte) []cachedLibrary {
	start := bytes.Index(data, []byte(cacheMagic))
	if start < 0 || len(data) < start+cacheHeader {
		return nil
	}

	count := int(binary.NativeEndian.Uint32(data[start+len(cacheMagic):]))
	if count > (len(data)-start-cacheEntryOffset)/cacheEntrySize {
		return nil
	}

	entries := make([]cachedLibrary, 0, count)
	for i := range count {
		entry := start + cacheEntryOffset + i*cacheEntrySize
		name, ok := cacheString(data, start, binary.NativeEndian.Uint32(data[entry+4:]))
		if !ok {
			continue
		}
		path, ok := cacheString(data, start, binary.NativeEndian.Uint32(data[entry+8:]))
		if !ok {
			continue
		}
		entries = append(entries, cachedLibrary{name: name, path: path})
	}

	return entries
}

// cacheString reads the NUL-terminated string at offset, which is counted from
// the cache header at start.
func cacheString(data []byte, start int, offset uint32) (string, bool) {
	index := start + int(offset)
	if index >= len(data) {
		return "", false
	}

	rest := data[index:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return "", false
	}

	return string(rest[:end]), true
}

// sameClass reports whether path is a shared object of the requested ELF class.
func sameClass(path string, class elf.Class) bool {
	f, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	return f.Class == class
}
