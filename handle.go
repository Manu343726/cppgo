package cppgo

// Handle refers to memory whose layout the Go side cannot see: a C++ instance,
// a context, an iterator.
//
// It holds an address as a uintptr, so the Go runtime never treats foreign memory
// as memory of its own. Nothing behind the Handle is Go's, and no dereference of
// it is checked.
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
// A finalizer is deliberately absent: it would run at an unpredictable moment,
// on any goroutine, and would free an object while the library it belongs to may
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
