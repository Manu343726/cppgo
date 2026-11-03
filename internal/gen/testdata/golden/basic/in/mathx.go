// Package mathx is the golden test fixture for the generator. It is parsed,
// never compiled: the declarations have no bodies.
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx

// AddI64 adds two integers.
//
//cppgo:external add_i64
func AddI64(a, b int64) int64

// Dot is the dot product of two slices.
//
//cppgo:external dot
func Dot(a, b []float64) (float64, error)

// Toggle flips a flag.
//
//cppgo:external toggle
func Toggle(flag bool) bool

// Init initialises the library.
//
//cppgo:external init
func Init(name string)

// Lookup returns an opaque object.
//
//cppgo:external lookup
func Lookup(key string, kind int32) cppgo.Handle

// Move translates a point.
//
//cppgo:external move
func Move(p *Point, dx, dy float64)

// Status reports a status code.
//
//cppgo:external status
func Status() error

// Point is passed by pointer, so the C++ side declares it.
type Point struct {
	X, Y float64
}
