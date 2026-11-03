// Package mathx binds mathx::Vector directly, with no extern "C" layer in
// between: this is the golden case of the direct ABI mode.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ../../../../../cxx/testdata/mathx/mathx.hpp

// Vector mirrors mathx::Vector.
//
//cppgo:class mathx::Vector
type Vector cppgo.Handle

// New constructs a Vector.
//
//cppgo:ctor
func New(x, y float64) (Vector, error)

// Norm returns the Euclidean norm.
//
//cppgo:method norm
func (v Vector) Norm() float64

// Dot returns the dot product with another vector's components.
//
//cppgo:method dot
func (v Vector) Dot(ys []float64) (float64, error)

// Destroy destroys the object.
//
//cppgo:dtor
func (v Vector) Destroy()

// AddInts adds two ints, picking the int overload of mathx::add.
//
//cppgo:function mathx::add
func AddInts(a, b int32) int32

// AddFloats adds two doubles, picking the double overload.
//
//cppgo:function mathx::add
func AddFloats(a, b float64) float64

// Dims is mathx::Vector::dims, a static member function: no this.
//
//cppgo:function mathx::Vector::dims
func Dims() int32

// IsAxis takes an enum and a bool and returns a bool.
//
//cppgo:function mathx::is_axis
func IsAxis(axis int32, strict bool) (bool, error)
