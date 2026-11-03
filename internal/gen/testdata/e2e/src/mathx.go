// Package mathx exercises the generator.
package mathx

//cppgo:library libexp

// AddI64 adds two integers.
//
//cppgo:external add_i64
func AddI64(a, b int64) int64

// Mix mixes int and float arguments.
//
//cppgo:external mix
func Mix(a int64, x float64, b int64, y float32) float64

// Scale scales a slice.
//
//cppgo:external scale
func Scale(v []float64, k float64) (float64, error)

// Boom always fails.
//
//cppgo:external boom
func Boom(msg string) error

// SumU8 sums bytes.
//
//cppgo:external sum_u8
func SumU8(v []uint8) uint64

// SetPoint writes a point.
//
//cppgo:external set_point
func SetPoint(p *Point, x, y float64)

// Point is a struct passed by pointer.
type Point struct {
	X, Y float64
}
