package mathx

import (
	"testing"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/genpkg/fake"
)

func init() {
	cppgo.Symbols["cppgo_add_i64"] = fake.AddI64Addr
	cppgo.Symbols["cppgo_mix"] = fake.MixAddr
	cppgo.Symbols["cppgo_scale"] = fake.ScaleAddr
	cppgo.Symbols["cppgo_boom"] = fake.BoomAddr
	cppgo.Symbols["cppgo_sum_u8"] = fake.SumU8Addr
	cppgo.Symbols["cppgo_set_point"] = fake.SetPointAddr
}

func TestGenerated(t *testing.T) {
	if got := AddI64(2, 40); got != 42 {
		t.Fatalf("AddI64 = %d", got)
	}
	if got := AddI64(-5, 5); got != 0 {
		t.Fatalf("AddI64 neg = %d", got)
	}
	if got := Mix(1, 10, 2, 0.5); got != 2059.5 {
		t.Fatalf("Mix = %v want 2059.5", got)
	}
	v := []float64{1, 2, 3, 4}
	if got, err := Scale(v, 2.5); err != nil || got != 10 {
		t.Fatalf("Scale = %v, %v", got, err)
	}
	if got := SumU8([]byte{1, 2, 3, 4}); got != 10 {
		t.Fatalf("SumU8 = %d", got)
	}
	if got := SumU8(nil); got != 0 {
		t.Fatalf("SumU8(nil) = %d", got)
	}
	p := &Point{}
	SetPoint(p, 1.5, -2.5)
	if p.X != 1.5 || p.Y != -2.5 {
		t.Fatalf("SetPoint = %+v", p)
	}
	err := Boom("hi")
	if err == nil || err.Error() != "boom: call failed with status 1" {
		t.Fatalf("Boom = %v", err)
	}
	cppgo.SetLastError("kaboom")
	err = Boom("hi")
	if err == nil || err.Error() != "boom: kaboom" {
		t.Fatalf("Boom with message = %v", err)
	}
	cppgo.SetLastError("")
}
