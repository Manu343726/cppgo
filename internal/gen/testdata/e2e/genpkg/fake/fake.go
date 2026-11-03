// Package fake provides assembly stand-ins for the C++ shim, so the generated
// trampolines can be exercised without cgo. Each function has the C calling
// convention; only the entry address is used by the tests.
package fake

func AddI64(a int64, b int64) int64

func Mix(a int64, x float64, b int64, y float32) float64

func Scale(ptr *float64, n uintptr, k float64, out *float64) int32

func Boom(s *byte, n uintptr) int32

func SumU8(p *byte, n uintptr) uint64

func SetPoint(p *Point, x float64, y float64)

// Point is an opaque stand-in for the bound package's struct.
type Point struct{ X, Y float64 }

var (
	AddI64Addr   uintptr
	MixAddr      uintptr
	ScaleAddr    uintptr
	BoomAddr     uintptr
	SumU8Addr    uintptr
	SetPointAddr uintptr
)
