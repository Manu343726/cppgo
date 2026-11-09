//go:build stackalloc

package stackalloc

import (
	"runtime"
	"unsafe"
)

// Slab is the alternative that removes the per-object allocation without
// claiming stack semantics: one pinned chunk of fixed-size blocks, handed out
// from a free list. A block's address is stable for as long as the slab lives,
// because the chunk is pinned once and never reallocated.
//
// It is not safe for concurrent use; the benchmark is single-goroutine and the
// point of the measurement is the allocation count. A production version would
// need a lock or a per-P slab pool, which is what sync.Pool already is.
type Slab struct {
	chunk  []byte
	free   []uintptr
	pinner runtime.Pinner
}

// NewSlab allocates perChunk blocks of blockSize bytes, aligns each to 8 and
// pins the whole chunk with a single Pinner. One Pinner pin covers the
// allocation: the runtime records the pin per heap object, not per byte.
func NewSlab(blockSize, perChunk int) *Slab {
	if perChunk < 1 {
		perChunk = 1
	}
	s := &Slab{chunk: make([]byte, blockSize*perChunk)}
	s.pinner.Pin(&s.chunk[0])
	s.free = make([]uintptr, 0, perChunk)
	base := uintptr(unsafe.Pointer(&s.chunk[0]))
	for i := perChunk - 1; i >= 0; i-- {
		addr := base + uintptr(i*blockSize)
		addr += (8 - addr%8) % 8
		s.free = append(s.free, addr)
	}
	return s
}

// Get returns a free block, or 0 when the slab is exhausted.
func (s *Slab) Get() uintptr {
	n := len(s.free)
	if n == 0 {
		return 0
	}
	addr := s.free[n-1]
	s.free = s.free[:n-1]
	return addr
}

// Put returns a block to the free list.
func (s *Slab) Put(addr uintptr) {
	s.free = append(s.free, addr)
}

// Close unpins the chunk and drops it.
func (s *Slab) Close() {
	s.pinner.Unpin()
	s.chunk = nil
	s.free = nil
}
