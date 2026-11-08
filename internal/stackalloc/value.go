//go:build stackalloc

package stackalloc

import (
	"runtime"
	"unsafe"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
)

// CellSize is sizeof(cppgo_cell) in testdata/stack_fixture.cc.
const CellSize = 16

// Cell is the value form of the runtime's Object: the storage for a C++ value
// lives in the Go value itself instead of in a pinned heap allocation, and the
// methods take the address of that storage for the duration of one call.
//
// It is what the generator would emit if it could keep the object on the Go
// stack. docs/dev/stack-allocation.md measures it and states the cases in which
// it is not safe.
type Cell struct {
	// The zero-size field makes the struct's alignment 8, which the C++
	// object needs. A [16]byte alone is aligned 1, and the compiler is then
	// free to place a local at an address the C++ constructor would fault on.
	_   [0]uint64
	buf [CellSize]byte
}

// Addr returns the address of the inline storage. It is only meaningful for as
// long as the Cell stays where it is: a goroutine stack growth moves it.
func (c *Cell) Addr() uintptr { return uintptr(unsafe.Pointer(&c.buf[0])) }

// Construct placement-news a cppgo_cell into c.
//
// ProbeStack runs before the address is taken, which is the order the design
// already requires: the probe grows the stack, and a pointer formed before it
// would name the frame that was moved.
//
//go:noinline
func (c *Cell) Construct(fn unsafe.Pointer, x, y float64) int32 {
	cppgo.ProbeStack()
	var args asm.Args
	args.I[0] = c.Addr()
	args.F[0] = x
	args.F[1] = y
	status, _ := asm.Call(fn, &args)
	return int32(status)
}

// Norm calls cppgo_cell_norm against the inline storage.
//
//go:noinline
func (c *Cell) Norm(fn unsafe.Pointer) float64 {
	cppgo.ProbeStack()
	var result float64
	var args asm.Args
	args.I[0] = c.Addr()
	args.I[1] = uintptr(unsafe.Pointer(&result))
	asm.Call(fn, &args)
	// runtime.KeepAlive is an intrinsic: unlike cppgo.KeepAlive it does not box
	// its arguments, so it keeps the receiver and the result slot alive without
	// moving either to the heap.
	runtime.KeepAlive(c)
	runtime.KeepAlive(&result)
	return result
}

// X calls cppgo_cell_x against the inline storage.
//
//go:noinline
func (c *Cell) X(fn unsafe.Pointer) float64 {
	cppgo.ProbeStack()
	var result float64
	var args asm.Args
	args.I[0] = c.Addr()
	args.I[1] = uintptr(unsafe.Pointer(&result))
	asm.Call(fn, &args)
	runtime.KeepAlive(c)
	runtime.KeepAlive(&result)
	return result
}

// Destroy calls the C++ destructor against the inline storage.
//
//go:noinline
func (c *Cell) Destroy(fn unsafe.Pointer) {
	cppgo.ProbeStack()
	var args asm.Args
	args.I[0] = c.Addr()
	asm.Call(fn, &args)
	runtime.KeepAlive(c)
}

// NewCell constructs the object at the storage of a local and returns the
// value. The return copies the C++ object's bytes to the caller's frame, which
// is the relocation the analysis is about: the constructor ran at a different
// address than the value the caller ends up holding.
//
//go:noinline
func NewCell(construct unsafe.Pointer, x, y float64) Cell {
	var c Cell
	c.Construct(construct, x, y)
	return c
}

// vectorShape is the Go half of the generator's shape for a bound record: a
// pointer to the C++ storage behind a Go wrapper. It is reproduced here so the
// benchmark measures the emitted form, not an approximation of it.
type vectorShape struct {
	obj *cppgo.Object
}

// NewVectorShape is NewVector's emitted shape, with the thunk addresses passed
// in instead of resolved from a loaded symbol table:
//
//	constructed := false
//	var obj *cppgo.Object
//	obj = cppgo.NewObject(16, 8, func() { ... obj.Addr() ... })
//	status := thunk(construct, obj.Addr(), x, y)
//	constructed = true
//	return &Vector{obj: obj}, nil
//
// Every allocation the escape analysis reports for the generated file has its
// counterpart here. docs/dev/stack-allocation.md carries the transcript.
//
//go:noinline
func NewVectorShape(construct, destroy unsafe.Pointer, x, y float64) *vectorShape {
	cppgo.ProbeStack()
	constructed := false
	var obj *cppgo.Object
	obj = cppgo.NewObject(16, 8, func() {
		if !constructed {
			return
		}
		cppgo.ProbeStack()
		var args asm.Args
		args.I[0] = obj.Addr()
		asm.Call(destroy, &args)
	})
	status, _ := func() (uintptr, float64) {
		var args asm.Args
		args.I[0] = obj.Addr()
		args.F[0] = x
		args.F[1] = y
		return asm.Call(construct, &args)
	}()
	if status != 0 {
		obj.Release()
		return nil
	}
	constructed = true
	return &vectorShape{obj: obj}
}

// Norm calls the method through the stored object, the way the generated
// accessor does: the result is written through an out-pointer that the
// trampoline's declaration leaks, and the object is kept alive with
// cppgo.KeepAlive.
//
//go:noinline
func (v *vectorShape) Norm(norm unsafe.Pointer) float64 {
	cppgo.ProbeStack()
	result := callOutFloat(uintptr(norm), v.obj.Addr())
	cppgo.KeepAlive(v, &result)
	return result
}

// Close is the generated Close: run the destructor once and drop the reference.
//
//go:noinline
func (v *vectorShape) Close(destroy unsafe.Pointer) {
	obj := v.obj
	v.obj = nil
	obj.Release()
	_ = destroy
}
