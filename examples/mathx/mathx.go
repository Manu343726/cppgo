// Package mathx binds a small C++17 class directly, with no extern "C" layer
// in between: this is what cppgo's direct ABI mode looks like end to end.
//
// The Go declarations below are the whole interface. `cppgo gen` reads the C++
// header through libclang, resolves every declaration to its mangled symbol,
// and validates the shape before it emits anything: an inline member, a virtual
// one, a std:: type or a struct passed by value is a build error here, not a
// crash at the first call.
//
// The generated files are zz_cppgo.gen.go (the wrappers) and zz_cppgo_amd64.s
// (one NOSPLIT trampoline per symbol). This file is rewritten in place by the
// generator: each declaration below gains a one-line body that delegates to its
// wrapper. Keep the directives; the bodies are regenerated.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header include/mathx/vector.hpp

// Vector mirrors mathx::Vector. Declaring it over cppgo.Handle — which is a
// uintptr — is what makes the receiver *be* the object pointer: no struct, no
// field to remember, and nothing for the collector to trace.
//
//cppgo:class mathx::Vector
type Vector cppgo.Handle

// New constructs a Vector: `this` in RDI, x and y in XMM0 and XMM1.
//
// The object is allocated by the generated wrapper, sized with sizeof from
// libclang (16 bytes here) and held in Go storage the package keeps reachable
// until Destroy drops it. Construction cannot fail, but the error result is
// still declared: it is how a failed library load is reported, and it is the
// same shape every other constructor in this project has.
//
//cppgo:ctor
func New(x, y float64) (Vector, error) { return cppgoCallNew(x, y) }

// Destroy runs the destructor and releases the Go storage behind the handle.
// Every constructor needs one of these: a Handle carries no ownership and
// nothing frees the object when the value becomes unreachable.
//
//cppgo:dtor
func (v Vector) Destroy() { cppgoCallDestroy(v) }

// X returns the x component. A const member: the call is
// _ZNK5mathx6Vector1xEv, with the K that marks it const.
//
//cppgo:method x
func (v Vector) X() float64 { return cppgoCallX(v) }

// Y returns the y component.
//
//cppgo:method y
func (v Vector) Y() float64 { return cppgoCallY(v) }

// Norm returns the Euclidean norm.
//
//cppgo:method norm
func (v Vector) Norm() float64 { return cppgoCallNorm(v) }

// Dot returns the dot product of v with ys. The slice crosses as a pointer and
// a length — no copy, no marshalling — so the C++ side sees Go's backing array
// directly.
//
//cppgo:method dot
func (v Vector) Dot(ys []float64) float64 { return cppgoCallDot(v, ys) }

// Scale multiplies both components by k, in place: a non-const member, the same
// symbol shape as a const one minus the K.
//
//cppgo:method scale
func (v Vector) Scale(k float64) { cppgoCallScale(v, k) }

// Dims is mathx::Vector::dims, a static member function. It has no `this`, so
// it is bound with //cppgo:function rather than //cppgo:method.
//
//cppgo:function mathx::Vector::dims
func Dims() int32 { return cppgoCallDims() }
