//go:build stackalloc

package stackalloc

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
)

// benchSink keeps results observable so the compiler cannot delete the loops
// under test.
var benchSink any

// BenchmarkCellLifecycle is the value form, end to end: a Cell in the calling
// frame, constructed by the C++ thunk, one method call, and the destructor.
// It is the number the question asks about: 0 allocations, no heap.
func BenchmarkCellLifecycle(b *testing.B) {
	sym := fixture(b)
	construct := sym("cppgo_cell_construct")
	norm := sym("cppgo_cell_norm")
	destroy := sym("cppgo_cell_destroy")

	var sink float64
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var c Cell
		c.Construct(construct, 3, 4)
		sink += c.Norm(norm)
		c.Destroy(destroy)
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkObjectLifecycle is the emitted shape, end to end: NewObject pins a
// heap allocation, the closure is the release function, &Vector is the Go
// wrapper, and Release runs the destructor.
func BenchmarkObjectLifecycle(b *testing.B) {
	sym := fixture(b)
	construct := sym("cppgo_cell_construct")
	norm := sym("cppgo_cell_norm")
	destroy := sym("cppgo_cell_destroy")

	var sink float64
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		v := NewVectorShape(construct, destroy, 3, 4)
		sink += v.Norm(norm)
		v.Close(destroy)
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkObjectNewRelease prices the storage alone: NewObject with no
// destructor and an immediate Release. It is what "the per-object allocation"
// costs before any C++ work happens.
func BenchmarkObjectNewRelease(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		obj := cppgo.NewObject(16, 8, nil)
		obj.Release()
	}
}

// BenchmarkCellZero prices the value form's storage alone: a local Cell and its
// address. There is nothing to allocate.
func BenchmarkCellZero(b *testing.B) {
	var sink uintptr
	b.ReportAllocs()
	for b.Loop() {
		var c Cell
		sink += c.Addr()
	}
	benchSink = sink
}

// BenchmarkSlabLifecycle is the alternative without stack semantics: a block
// from a pinned slab, the same three calls, and the block returned. The
// allocation happens once per slab, so the loop is 0 allocs/op in steady state.
func BenchmarkSlabLifecycle(b *testing.B) {
	sym := fixture(b)
	construct := sym("cppgo_cell_construct")
	norm := sym("cppgo_cell_norm")
	destroy := sym("cppgo_cell_destroy")

	slab := NewSlab(32, 1024)
	b.Cleanup(slab.Close)

	var sink float64
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		addr := slab.Get()
		var args asm.Args
		args.I[0] = addr
		args.F[0], args.F[1] = 3, 4
		asm.Call(construct, &args)

		var result float64
		args = asm.Args{}
		args.I[0] = addr
		args.I[1] = uintptr(unsafe.Pointer(&result))
		asm.Call(norm, &args)

		args = asm.Args{}
		args.I[0] = addr
		asm.Call(destroy, &args)

		sink += result
		slab.Put(addr)
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkCellNorm and BenchmarkObjectNorm are the per-call comparison with the
// object's creation outside the loop: what one accessor costs in each form. The
// difference is cppgo.KeepAlive's boxing in the emitted shape, not the object.
func BenchmarkCellNorm(b *testing.B) {
	sym := fixture(b)
	norm := sym("cppgo_cell_norm")

	var c Cell
	c.Construct(sym("cppgo_cell_construct"), 3, 4)
	defer c.Destroy(sym("cppgo_cell_destroy"))

	var sink float64
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		sink += c.Norm(norm)
	}

	b.StopTimer()
	benchSink = sink
}

func BenchmarkObjectNorm(b *testing.B) {
	sym := fixture(b)
	norm := sym("cppgo_cell_norm")

	v := NewVectorShape(sym("cppgo_cell_construct"), sym("cppgo_cell_destroy"), 3, 4)
	defer v.Close(sym("cppgo_cell_destroy"))

	var sink float64
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		sink += v.Norm(norm)
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkPointByValue is the case that already needs no allocation at all:
// a trivially copyable C++ record mirrored as a Go struct, passed and returned
// by value.
func BenchmarkPointByValue(b *testing.B) {
	sym := fixture(b)
	midpoint := sym("cppgo_point_midpoint")

	a := point{X: 1, Y: 2}
	c := point{X: 3, Y: 4}

	var sink float64
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var result point
		var args asm.Args
		args.I[0] = uintptr(unsafe.Pointer(&a))
		args.I[1] = uintptr(unsafe.Pointer(&c))
		args.I[2] = uintptr(unsafe.Pointer(&result))
		asm.Call(midpoint, &args)
		runtime.KeepAlive(&a)
		runtime.KeepAlive(&c)
		sink += result.X
	}

	b.StopTimer()
	benchSink = sink
}

// point mirrors cppgo_point: the Go struct *is* the C layout.
type point struct {
	X float64
	Y float64
}
