package cppgo

// Handle refers to memory whose layout the Go side cannot see: a C++ instance,
// a context, an iterator.
//
// It is a uintptr on purpose. The memory belongs to the foreign side, or to a Go
// allocation whose layout mirrors it, and an unsafe.Pointer would invite the
// collector and vet's unsafeptr check to reason about memory outside the Go
// heap.
//
// # Lifetimes
//
// A Handle carries no ownership. No finalizer frees the object when the Handle
// becomes unreachable, and the runtime keeps nothing alive on its behalf, so
// every constructor needs a matching destroy and the Go code calls it:
//
//	type Vector struct{ h cppgo.Handle }
//
//	func NewVector(x, y float64) (*Vector, error) {
//		var out cppgo.Handle
//		if err := createVector(&out, x, y); err != nil {
//			return nil, err
//		}
//		return &Vector{h: out}, nil
//	}
//
//	func (v *Vector) Close() error { return destroyVector(v.h) }
//
// A finalizer is deliberately absent. It would run at an unpredictable moment
// on an arbitrary goroutine, and would call into C++ while the library may
// already be unloading.
//
// # Zero value
//
// The zero Handle is the null pointer, and it is what a failing constructor
// leaves behind. Foreign code rejects it. A generated binding never hands a
// Handle out in an exported signature.
type Handle uintptr

// IsNil reports whether h is the null handle.
func (h Handle) IsNil() bool { return h == 0 }
